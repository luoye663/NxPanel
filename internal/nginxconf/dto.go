package nginxconf

type NginxConfResponse struct {
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

type SaveNginxConfRequest struct {
	Content         string `json:"content"`
	ExpectedHash    string `json:"expected_hash"`
	DangerConfirmed bool   `json:"danger_confirmed"`
}

type SaveNginxConfResponse struct {
	Hash        string `json:"hash"`
	OperationID string `json:"operation_id"`
}

type ParameterValue struct {
	Key          string   `json:"key"`
	Value        string   `json:"value"`
	DefaultValue string   `json:"default_value"`
	Description  string   `json:"description"`
	Unit         string   `json:"unit"`
	Group        string   `json:"group"`
	Tooltip      string   `json:"tooltip"`
	Options      []string `json:"options,omitempty"`
	Clearable    bool     `json:"clearable,omitempty"`
}

type NginxParametersResponse struct {
	Parameters []ParameterValue `json:"parameters"`
	ConfPath   string           `json:"conf_path"`
}

type SaveNginxParametersRequest struct {
	Parameters map[string]string `json:"parameters"`
}

type SaveNginxParametersResponse struct {
	Parameters []ParameterValue `json:"parameters"`
	ConfPath   string           `json:"conf_path"`
	OperationID string          `json:"operation_id"`
}
