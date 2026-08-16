package upstream

type SaveRequest struct {
	Name                    string          `json:"name,omitempty"`
	Algorithm               string          `json:"algorithm"`
	HashKey                 string          `json:"hash_key"`
	Consistent              bool            `json:"consistent"`
	Keepalive               int             `json:"keepalive"`
	KeepaliveRequests       int             `json:"keepalive_requests"`
	KeepaliveTimeoutSeconds int             `json:"keepalive_timeout_seconds"`
	AdvancedDirectives      string          `json:"advanced_directives"`
	Servers                 []ServerRequest `json:"servers"`
}

type ServerRequest struct {
	Address            string `json:"address"`
	Weight             int    `json:"weight"`
	MaxFails           int    `json:"max_fails"`
	FailTimeoutSeconds int    `json:"fail_timeout_seconds"`
	Backup             bool   `json:"backup"`
	Down               bool   `json:"down"`
	SortOrder          int    `json:"sort_order"`
}

type Server struct {
	ID                 string `json:"id"`
	Address            string `json:"address"`
	Weight             int    `json:"weight"`
	MaxFails           int    `json:"max_fails"`
	FailTimeoutSeconds int    `json:"fail_timeout_seconds"`
	Backup             bool   `json:"backup"`
	Down               bool   `json:"down"`
	SortOrder          int    `json:"sort_order"`
}

type Upstream struct {
	ID                      string   `json:"id"`
	Name                    string   `json:"name"`
	Algorithm               string   `json:"algorithm"`
	HashKey                 string   `json:"hash_key"`
	Consistent              bool     `json:"consistent"`
	Keepalive               int      `json:"keepalive"`
	KeepaliveRequests       int      `json:"keepalive_requests"`
	KeepaliveTimeoutSeconds int      `json:"keepalive_timeout_seconds"`
	AdvancedDirectives      string   `json:"advanced_directives"`
	Servers                 []Server `json:"servers"`
	CreatedAt               string   `json:"created_at"`
	UpdatedAt               string   `json:"updated_at"`
	ReferenceCount          int      `json:"reference_count"`
}

type WriteResult struct {
	Upstream    *Upstream `json:"upstream,omitempty"`
	OperationID string    `json:"operation_id"`
}

type ValidateResult struct {
	RenderedBlock string `json:"rendered_block"`
	Preview       string `json:"preview"`
}

type SyncResult struct {
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
}

type StatusResult struct {
	DesiredHash string  `json:"desired_hash"`
	AppliedHash string  `json:"applied_hash"`
	AppliedAt   *string `json:"applied_at"`
	Synced      bool    `json:"synced"`
	Path        string  `json:"path"`
}
