package ssl

import (
	"context"
	"fmt"
	"strings"

	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

func dependencyHash(checks []FileChange) string {
	var text strings.Builder
	for _, c := range checks {
		text.WriteString(c.Path)
		text.WriteByte(0)
		text.WriteString(c.ExpectedHash)
		text.WriteByte(0)
	}
	return nginxpatch.Hash(text.String())
}

func (svc *Service) resolveCertificate(ctx context.Context, content, domain string) (nginxpatch.CertificateReference, []FileChange, error) {
	dump, err := svc.agent.NginxDump(ctx)
	if err != nil {
		return nginxpatch.CertificateReference{}, nil, fmt.Errorf("cannot inspect expanded TLS configuration through Agent")
	}
	if err := nginxpatch.ValidateCertificateRuntime(dump); err != nil {
		return nginxpatch.CertificateReference{}, nil, err
	}
	paths, err := nginxpatch.CertificateIncludes(content, domain)
	if err != nil {
		return nginxpatch.CertificateReference{}, nil, err
	}
	seen := map[string]bool{}
	total := len(content)
	var checks []FileChange
	for len(paths) > 0 {
		path := paths[0]
		paths = paths[1:]
		if seen[path] {
			return nginxpatch.CertificateReference{}, nil, fmt.Errorf("repeated or cyclic include ownership is ambiguous")
		}
		seen[path] = true
		if len(seen) > 16 {
			return nginxpatch.CertificateReference{}, nil, fmt.Errorf("include graph exceeds safety budget")
		}
		data, err := svc.agent.ReadFile(ctx, path)
		if err != nil {
			return nginxpatch.CertificateReference{}, nil, fmt.Errorf("include cannot be read through Agent path policy")
		}
		total += len(data)
		if total > nginxpatch.MaxConfigBytes {
			return nginxpatch.CertificateReference{}, nil, fmt.Errorf("include graph exceeds configuration size budget")
		}
		more, err := nginxpatch.InspectIncludeFragment(string(data))
		if err != nil {
			return nginxpatch.CertificateReference{}, nil, err
		}
		paths = append(paths, more...)
		checks = append(checks, FileChange{Type: "check", Path: path, FollowSymlink: true, ExpectedHash: nginxpatch.Hash(string(data))})
	}
	ref := nginxpatch.InspectResolvedCertificate(content, domain)
	if ref.ReadOnlyReason != "" {
		return ref, nil, fmt.Errorf("%s", ref.ReadOnlyReason)
	}
	return ref, checks, nil
}
