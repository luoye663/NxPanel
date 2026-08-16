package upload

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
)

const (
	MaxPathBytes      = 4096
	MaxFilenameBytes  = 255
	MultipartOverhead = 64 * 1024
	JSONOverhead      = MaxPathBytes + 1024
)

var (
	ErrTooLarge  = errors.New("upload exceeds configured limit")
	ErrMalformed = errors.New("malformed upload")
)

type File struct {
	Path     string
	Filename string
	Size     int64
	File     *os.File
}

func (f *File) Close() error {
	if f == nil || f.File == nil {
		return nil
	}
	name := f.File.Name()
	err := f.File.Close()
	if removeErr := os.Remove(name); err == nil {
		err = removeErr
	}
	return err
}

func Parse(r *http.Request, maxBytes int64) (*File, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid content type", ErrMalformed)
	}
	switch mediaType {
	case "multipart/form-data":
		return parseMultipart(r, params["boundary"], maxBytes)
	case "application/json":
		return parseJSON(r, maxBytes)
	default:
		return nil, fmt.Errorf("%w: unsupported content type", ErrMalformed)
	}
}

func parseMultipart(r *http.Request, boundary string, maxBytes int64) (*File, error) {
	if boundary == "" {
		return nil, fmt.Errorf("%w: missing multipart boundary", ErrMalformed)
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes+MultipartOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("%w: invalid multipart body", ErrMalformed)
	}
	var result File
	complete := false
	defer func() {
		if !complete {
			_ = result.Close()
		}
	}()
	var pathSeen, fileSeen bool
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, classifyReadError(err)
		}
		name := part.FormName()
		switch name {
		case "path":
			if pathSeen || part.FileName() != "" {
				part.Close()
				return nil, fmt.Errorf("%w: duplicate or invalid path part", ErrMalformed)
			}
			pathSeen = true
			value, readErr := io.ReadAll(io.LimitReader(part, MaxPathBytes+1))
			part.Close()
			if readErr != nil {
				return nil, classifyReadError(readErr)
			}
			if len(value) == 0 || len(value) > MaxPathBytes {
				return nil, fmt.Errorf("%w: invalid path length", ErrMalformed)
			}
			result.Path = string(value)
		case "file":
			if fileSeen || part.FileName() == "" {
				part.Close()
				return nil, fmt.Errorf("%w: duplicate or invalid file part", ErrMalformed)
			}
			fileSeen = true
			result.Filename = part.FileName()
			if len(result.Filename) > MaxFilenameBytes {
				part.Close()
				return nil, fmt.Errorf("%w: filename too long", ErrMalformed)
			}
			tmp, createErr := os.CreateTemp("", "nxpanel-upload-*")
			if createErr != nil {
				part.Close()
				return nil, createErr
			}
			result.File = tmp
			result.Size, err = copyWithinLimit(tmp, part, maxBytes)
			part.Close()
			if err != nil {
				result.Close()
				return nil, err
			}
		default:
			part.Close()
			result.Close()
			return nil, fmt.Errorf("%w: unexpected multipart part", ErrMalformed)
		}
	}
	if !pathSeen || !fileSeen {
		result.Close()
		return nil, fmt.Errorf("%w: path and file parts are required", ErrMalformed)
	}
	if _, err := result.File.Seek(0, io.SeekStart); err != nil {
		result.Close()
		return nil, err
	}
	complete = true
	return &result, nil
}

func parseJSON(r *http.Request, maxBytes int64) (*File, error) {
	encodedLimit := int64(base64.StdEncoding.EncodedLen(int(maxBytes)))
	r.Body = http.MaxBytesReader(nil, r.Body, encodedLimit+JSONOverhead)
	var body struct {
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return nil, classifyReadError(err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if body.Path == "" || len(body.Path) > MaxPathBytes {
		return nil, fmt.Errorf("%w: invalid path length", ErrMalformed)
	}
	if int64(len(body.ContentBase64)) > encodedLimit {
		return nil, ErrTooLarge
	}
	tmp, err := os.CreateTemp("", "nxpanel-upload-*")
	if err != nil {
		return nil, err
	}
	result := &File{Path: body.Path, File: tmp}
	result.Size, err = copyWithinLimit(tmp, base64.NewDecoder(base64.StdEncoding, strings.NewReader(body.ContentBase64)), maxBytes)
	if err != nil {
		result.Close()
		if errors.Is(err, ErrTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: invalid base64 content", ErrMalformed)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		result.Close()
		return nil, err
	}
	return result, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: request must contain one JSON object", ErrMalformed)
	}
	return nil
}

func copyWithinLimit(dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	written, err := io.Copy(dst, io.LimitReader(src, maxBytes+1))
	if err != nil {
		return written, classifyReadError(err)
	}
	if written > maxBytes {
		return written, ErrTooLarge
	}
	return written, nil
}

func classifyReadError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return ErrTooLarge
	}
	return fmt.Errorf("%w: %w", ErrMalformed, err)
}
