package agent

import (
	"fmt"
	"io"
	"os"

	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

func (s *Server) rejectMaintenancePaths(paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		validated, err := s.policy.Validate(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(validated)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(validated)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, nginxpatch.MaxConfigBytes+1))
		_ = file.Close()
		if readErr != nil {
			return readErr
		}
		if nginxpatch.HasMaintenance(string(data)) {
			return fmt.Errorf("configuration is locked during maintenance; restore access first")
		}
	}
	return nil
}
