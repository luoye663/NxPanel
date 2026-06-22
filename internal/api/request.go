package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/luoye663/nxpanel/internal/app"
)

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := decodeJSON(r, dst)
	if err == nil {
		return true
	}
	WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "请求体格式错误", nil)
	return false
}

func DecodeJSONOptional(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := decodeJSON(r, dst)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "请求体格式错误", nil)
	return false
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
