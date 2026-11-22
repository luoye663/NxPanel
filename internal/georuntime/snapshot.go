package georuntime

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
)

const maxConfigBytes = 64 * 1024 * 1024

type Snapshot struct {
	Hash   string
	Prefix []byte
	Shared bool
	Legacy bool
}

func ReadSnapshot(source io.Reader) (Snapshot, error) {
	limited := &io.LimitedReader{R: source, N: maxConfigBytes + 1}
	hash := sha256.New()
	var prefix bytes.Buffer
	read := io.TeeReader(limited, hash)
	if _, err := io.Copy(&prefix, io.LimitReader(read, 4096)); err != nil {
		return Snapshot{}, err
	}
	marker := []byte("map " + CountryVariable + " ")
	shared, legacy := false, false
	reader := bufio.NewReader(io.MultiReader(bytes.NewReader(prefix.Bytes()), read))
	for {
		line, err := reader.ReadSlice('\n')
		shared = shared || bytes.HasPrefix(line, marker)
		line = bytes.TrimSpace(line)
		legacy = legacy || bytes.HasPrefix(line, []byte("geo $nx_")) && bytes.HasSuffix(line, []byte("_country {")) ||
			bytes.HasPrefix(line, []byte("geo $nx_geo_country_")) || bytes.Contains(line, []byte("NXPANEL-GEO-ACCESS-START")) ||
			bytes.HasPrefix(line, []byte("include ")) && bytes.Contains(line, []byte("/geo-access/sites/"))
		if err == io.EOF {
			break
		}
		if err != nil && err != bufio.ErrBufferFull {
			return Snapshot{}, err
		}
	}
	if limited.N == 0 {
		return Snapshot{}, fmt.Errorf("地域配置超过 64 MiB，无法自动迁移")
	}
	return Snapshot{Hash: fmt.Sprintf("%x", hash.Sum(nil)), Prefix: prefix.Bytes(), Shared: shared, Legacy: legacy}, nil
}

func Hash(content string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(content))) }
