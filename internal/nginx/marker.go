package nginx

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
)

type MarkerStatus struct {
	Valid      bool     `json:"valid"`
	Missing    []string `json:"missing"`
	Duplicated []string `json:"duplicated"`
}

type BlockPatch struct {
	Name string
	Body []byte
}

var markerNameRE = regexp.MustCompile(`^[A-Z0-9-]+$`)

var (
	ErrMarkerMissing    = errors.New("config marker missing")
	ErrMarkerDuplicated = errors.New("config marker duplicated")
)

func markerStart(name string) string { return MarkerPrefix + name + "-START" }
func markerEnd(name string) string   { return MarkerPrefix + name + "-END" }

func ReplaceMarkerBlock(content []byte, name string, newBody []byte) ([]byte, error) {
	if !markerNameRE.MatchString(name) {
		return nil, fmt.Errorf("invalid marker name: %s", name)
	}

	startToken := []byte(markerStart(name))
	endToken := []byte(markerEnd(name))

	startCount := bytes.Count(content, startToken)
	endCount := bytes.Count(content, endToken)
	if startCount == 0 || endCount == 0 {
		return nil, fmt.Errorf("%w: %s", ErrMarkerMissing, name)
	}
	if startCount > 1 || endCount > 1 {
		return nil, fmt.Errorf("%w: %s", ErrMarkerDuplicated, name)
	}

	startIdx := bytes.Index(content, startToken)
	if startIdx < 0 {
		return nil, fmt.Errorf("%w: %s", ErrMarkerMissing, name)
	}

	startLineEnd := bytes.IndexByte(content[startIdx:], '\n')
	if startLineEnd < 0 {
		return nil, fmt.Errorf("marker start line not closed: %s", name)
	}
	bodyStart := startIdx + startLineEnd + 1

	endIdxRel := bytes.Index(content[bodyStart:], endToken)
	if endIdxRel < 0 {
		return nil, fmt.Errorf("%w: %s", ErrMarkerMissing, name)
	}
	endIdx := bodyStart + endIdxRel
	bodyEnd := markerBodyEnd(content, bodyStart, endIdx)

	out := make([]byte, 0, len(content)-bodyEnd+bodyStart+len(newBody)+2)
	out = append(out, content[:bodyStart]...)
	out = append(out, ensureTrailingNewline(newBody)...)
	out = append(out, content[bodyEnd:]...)
	return out, nil
}

func ExtractMarkerBlock(content []byte, name string) ([]byte, error) {
	if !markerNameRE.MatchString(name) {
		return nil, fmt.Errorf("invalid marker name: %s", name)
	}

	startToken := []byte(markerStart(name))
	endToken := []byte(markerEnd(name))

	if bytes.Count(content, startToken) != 1 || bytes.Count(content, endToken) != 1 {
		return nil, fmt.Errorf("marker count invalid: %s", name)
	}

	startIdx := bytes.Index(content, startToken)
	startLineEnd := bytes.IndexByte(content[startIdx:], '\n')
	if startLineEnd < 0 {
		return nil, fmt.Errorf("marker start line not closed: %s", name)
	}
	bodyStart := startIdx + startLineEnd + 1

	endIdxRel := bytes.Index(content[bodyStart:], endToken)
	if endIdxRel < 0 {
		return nil, fmt.Errorf("marker end missing: %s", name)
	}
	endIdx := bodyStart + endIdxRel
	bodyEnd := markerBodyEnd(content, bodyStart, endIdx)
	return content[bodyStart:bodyEnd], nil
}

func markerBodyEnd(content []byte, bodyStart, endIdx int) int {
	lineStart := endIdx
	for lineStart > bodyStart && content[lineStart-1] != '\n' {
		lineStart--
	}
	if lineStart < endIdx && len(bytes.TrimSpace(content[lineStart:endIdx])) == 0 {
		return lineStart
	}
	return endIdx
}

func ValidateRequiredMarkers(content []byte, required []string) MarkerStatus {
	status := MarkerStatus{Valid: true}
	for _, name := range required {
		startCount := bytes.Count(content, []byte(markerStart(name)))
		endCount := bytes.Count(content, []byte(markerEnd(name)))
		switch {
		case startCount == 0 || endCount == 0:
			status.Valid = false
			status.Missing = append(status.Missing, name)
		case startCount > 1 || endCount > 1:
			status.Valid = false
			status.Duplicated = append(status.Duplicated, name)
		}
	}
	return status
}

func ApplyMarkerPatches(content []byte, patches []BlockPatch) ([]byte, error) {
	next := content
	for _, p := range patches {
		var err error
		next, err = ReplaceMarkerBlock(next, p.Name, p.Body)
		if err != nil {
			return nil, fmt.Errorf("replace marker %s: %w", p.Name, err)
		}
	}
	return next, nil
}

func ApplyOptionalMarkerPatches(content []byte, patches []BlockPatch) ([]byte, error) {
	next := content
	for _, p := range patches {
		var err error
		next, err = EnsureMarkerBlock(next, p.Name, p.Body)
		if err != nil {
			return nil, fmt.Errorf("ensure marker %s: %w", p.Name, err)
		}
	}
	return next, nil
}

func EnsureMarkerBlock(content []byte, name string, body []byte) ([]byte, error) {
	if !markerNameRE.MatchString(name) {
		return nil, fmt.Errorf("invalid marker name: %s", name)
	}
	startCount := bytes.Count(content, []byte(markerStart(name)))
	endCount := bytes.Count(content, []byte(markerEnd(name)))
	if startCount > 1 || endCount > 1 {
		return nil, fmt.Errorf("%w: %s", ErrMarkerDuplicated, name)
	}
	if startCount == 1 && endCount == 1 {
		return ReplaceMarkerBlock(content, name, body)
	}
	if startCount != endCount {
		return nil, fmt.Errorf("%w: %s", ErrMarkerMissing, name)
	}
	anchor, err := optionalMarkerAnchor(content, name)
	if err != nil {
		return nil, err
	}
	return InjectMarkerAfter(content, anchor, name, body)
}

func InjectMarkerAfter(content []byte, afterName, newName string, newBody []byte) ([]byte, error) {
	if !markerNameRE.MatchString(afterName) || !markerNameRE.MatchString(newName) {
		return nil, fmt.Errorf("invalid marker name")
	}

	startToken := []byte(markerStart(newName))
	if bytes.Count(content, startToken) > 0 {
		return content, nil
	}

	afterEndToken := []byte(markerEnd(afterName))
	endIdx := bytes.Index(content, afterEndToken)
	if endIdx < 0 {
		return nil, fmt.Errorf("%w: anchor marker %s", ErrMarkerMissing, afterName)
	}

	lineEnd := bytes.IndexByte(content[endIdx:], '\n')
	insertAt := endIdx + lineEnd + 1

	block := fmt.Sprintf("\n    %s\n%s    %s\n", markerStart(newName), string(ensureTrailingNewline(newBody)), markerEnd(newName))

	out := make([]byte, 0, len(content)+len(block))
	out = append(out, content[:insertAt]...)
	out = append(out, []byte(block)...)
	out = append(out, content[insertAt:]...)
	return out, nil
}

func RequiredSiteMarkers() []string {
	return []string{
		MarkerNameSite,
		MarkerNameListen,
		MarkerNameServerName,
		MarkerNameRoot,
		MarkerNameLog,
		MarkerNameRewrite,
		MarkerNameDocument,
	}
}

func optionalMarkerAnchor(content []byte, name string) (string, error) {
	switch name {
	case MarkerNameSSL:
		return MarkerNameListen, nil
	case MarkerNameForceHTTPS:
		return MarkerNameSSL, nil
	case MarkerNameHotlink:
		return MarkerNameRewrite, nil
	case MarkerNameAccessLimit:
		return existingOptionalAnchor(content, MarkerNameHotlink, MarkerNameRewrite), nil
	case MarkerNameACMEChallenge:
		return MarkerNameDocument, nil
	case MarkerNameMainLocation:
		return existingOptionalAnchor(content, MarkerNameACMEChallenge, MarkerNameDocument), nil
	case MarkerNameExtraLocations:
		return MarkerNameMainLocation, nil
	default:
		return "", fmt.Errorf("marker %s is not optional", name)
	}
}

func existingOptionalAnchor(content []byte, optionalName, fallbackName string) string {
	if bytes.Count(content, []byte(markerStart(optionalName))) == 1 && bytes.Count(content, []byte(markerEnd(optionalName))) == 1 {
		return optionalName
	}
	return fallbackName
}

func ensureTrailingNewline(b []byte) []byte {
	if len(b) == 0 {
		return []byte("\n")
	}
	if b[len(b)-1] == '\n' {
		return b
	}
	out := make([]byte, 0, len(b)+1)
	out = append(out, b...)
	out = append(out, '\n')
	return out
}
