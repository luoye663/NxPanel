package waf

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func parseIPOrCIDR(value string) bool {
	if strings.Contains(value, "/") {
		_, _, err := net.ParseCIDR(value)
		return err == nil
	}
	return net.ParseIP(value) != nil
}

// RenderSiteConfig emits a deterministic server-context include. Paths must
// already be constrained to provider-owned roots by Validate.
func RenderSiteConfig(p SitePolicy, allowedRoots []string) ([]byte, error) {
	if err := p.Validate(allowedRoots); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("# Managed by nxPanel WAF provider; manual changes will be overwritten.\n")
	if !p.Enabled {
		out.WriteString("modsecurity off;\n")
		return out.Bytes(), nil
	}
	out.WriteString("modsecurity on;\n")
	out.WriteString("modsecurity_rules '\n")
	writeRule := func(rule string) { out.WriteString("    " + rule + "\n") }
	// The provider owns this baseline file. Site-specific policy follows it so
	// the baseline cannot silently override the selected mode or limits.
	writeRule("Include " + p.ModSecurityConf)
	writeRule("SecRuleEngine " + p.Mode)
	writeRule("SecRequestBodyAccess On")
	writeRule("SecRequestBodyLimit " + strconv.FormatInt(p.RequestBodyLimit, 10))
	writeRule("SecRequestBodyNoFilesLimit " + strconv.FormatInt(p.NoFilesBodyLimit, 10))
	writeRule("SecResponseBodyAccess On")
	writeRule("SecResponseBodyLimit " + strconv.FormatInt(p.ResponseBodyLimit, 10))
	writeRule("SecDefaultAction \"phase:1,log,auditlog,pass\"")
	writeRule("SecDefaultAction \"phase:2,log,auditlog,pass\"")
	writeRule("SecAction \"id:900000,phase:1,nolog,pass,t:none,setvar:tx.blocking_paranoia_level=" + strconv.Itoa(p.ParanoiaLevel) + "\"")
	writeRule("SecAction \"id:900110,phase:1,nolog,pass,t:none,setvar:tx.inbound_anomaly_score_threshold=" + strconv.Itoa(p.InboundThreshold) + ",setvar:tx.outbound_anomaly_score_threshold=" + strconv.Itoa(p.OutboundThreshold) + "\"")
	if p.Audit.Enabled {
		writeRule("SecAuditEngine RelevantOnly")
		writeRule("SecAuditLogType Concurrent")
		writeRule("SecAuditLogStorageDir " + p.Audit.StorageDir)
		writeRule("SecAuditLogParts ABCDEFGHIJKZ")
		writeRule("SecAuditLogDirMode 0750")
		writeRule("SecAuditLogFileMode 0640")
	} else {
		writeRule("SecAuditEngine Off")
	}
	writeRule("Include " + p.CRSSetupPath)
	writeRule("Include " + p.CRSRulesGlob)
	if p.ResponseStatus != 403 {
		// CRS 4.x blocking evaluation rules carry their own status:403 action.
		// Updating those fixed rule IDs is safer than changing CRS default actions.
		writeRule("SecRuleUpdateActionById 949110 \"status:" + strconv.Itoa(p.ResponseStatus) + "\"")
		writeRule("SecRuleUpdateActionById 959100 \"status:" + strconv.Itoa(p.ResponseStatus) + "\"")
	}
	for i, e := range sortedExclusions(p.Exclusions) {
		if e.Path == "" && e.Param == "" && e.SourceIP == "" {
			writeRule("SecRuleRemoveById " + e.RuleID)
			continue
		}
		variable, operator := "REQUEST_URI", "@beginsWith "+e.Path
		if e.Param != "" {
			variable, operator = "ARGS_NAMES", "@streq "+e.Param
		} else if e.SourceIP != "" {
			variable = "REMOTE_ADDR"
			if strings.Contains(e.SourceIP, "/") {
				operator = "@ipMatch " + e.SourceIP
			} else {
				operator = "@streq " + e.SourceIP
			}
		}
		writeRule(fmt.Sprintf("SecRule %s \"%s\" \"id:%d,phase:1,nolog,pass,t:none,ctl:ruleRemoveById=%s\"", variable, operator, 910000000+i, e.RuleID))
	}
	out.WriteString("';\n")
	return out.Bytes(), nil
}
