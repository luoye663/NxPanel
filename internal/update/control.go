package update

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	Idle       = "idle"
	Prepared   = "prepared"
	Applying   = "applying"
	Trial      = "trial"
	Restoring  = "restoring"
	VerifyOld  = "verifying_previous"
	Committed  = "committed"
	RolledBack = "rolled_back"
	Failed     = "failed"
	Blocked    = "blocked"
)

type State struct {
	ID       string `json:"id,omitempty"`
	Phase    string `json:"phase"`
	Version  string `json:"version,omitempty"`
	Previous string `json:"previous,omitempty"`
	OldSlot  string `json:"old_slot,omitempty"`
	Snapshot bool   `json:"snapshot,omitempty"`
	DBPath   string `json:"db_path,omitempty"`
	Error    string `json:"error,omitempty"`
}

type Action struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	DBPath  string `json:"db_path,omitempty"`
}

func Home() string { return os.Getenv("NXPANEL_UPDATE_HOME") }

func controlDir(home string) string { return filepath.Join(home, "update-control") }

func ReadState(home string) (State, error) {
	if home == "" {
		return State{Phase: Idle}, nil
	}
	file, err := os.Open(filepath.Join(controlDir(home), "state.json"))
	if os.IsNotExist(err) {
		return State{Phase: Idle}, nil
	}
	if err != nil {
		return State{}, err
	}
	defer file.Close()
	var state State
	if err := Decode(io.LimitReader(file, MetadataMax), &state); err != nil {
		return State{}, err
	}
	switch state.Phase {
	case Idle:
		return state, nil
	case Prepared, Applying, Trial, Restoring, VerifyOld, Committed, RolledBack, Blocked:
		if validSlot("versions/"+state.ID) && validSlot(state.OldSlot) && ValidVersion(state.Version) && ValidVersion(state.Previous) &&
			(state.Phase == Prepared || validDatabasePath(state.DBPath)) {
			return state, nil
		}
	}
	return State{}, errors.New("invalid persistent update state")
}

func (state State) Busy() bool {
	return state.Phase == Applying || state.Phase == Trial || state.Phase == Restoring || state.Phase == VerifyOld || state.Phase == Blocked
}

func (state State) TrialVersion() string {
	if state.Phase == Trial {
		return state.Version
	}
	if state.Phase == VerifyOld {
		return state.Previous
	}
	return ""
}

func AwaitBoot(ctx context.Context, version string) (State, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := ReadState(Home())
		if err != nil {
			return state, err
		}
		if state.Phase == Committed || state.Phase == RolledBack {
			live, err := NewClient(Home()).Status(ctx)
			if err == nil && live.ID == state.ID && live.Phase == state.Phase {
				expected := state.Version
				if state.Phase == RolledBack {
					expected = state.Previous
				}
				if version != expected {
					return state, errors.New("active program version does not match the update journal")
				}
				return state, nil
			}
		} else if !state.Busy() || state.TrialVersion() == version {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-ticker.C:
		}
	}
}

func AwaitCommit(ctx context.Context, trial State) error {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	client := NewClient(Home())
	expected := Committed
	if trial.Phase == VerifyOld {
		expected = RolledBack
	}
	for {
		state, err := client.Status(ctx)
		if err == nil && state.ID == trial.ID && state.Phase == expected {
			return nil
		}
		if err == nil && (state.ID != trial.ID || state.Phase != trial.Phase) {
			return errors.New("update trial not committed")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type Client struct {
	http *http.Client
}

func NewClient(home string) *Client {
	if home == "" {
		return nil
	}
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", filepath.Join(controlDir(home), "updater.sock"))
		},
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: 2 * time.Minute}}
}

func (client *Client) Status(ctx context.Context) (State, error) {
	return client.request(ctx, http.MethodGet, "status", nil)
}

func (client *Client) Act(ctx context.Context, route string, action Action) (State, error) {
	body, err := json.Marshal(action)
	if err != nil {
		return State{}, err
	}
	return client.request(ctx, http.MethodPost, route, bytes.NewReader(body))
}

func (client *Client) Stage(ctx context.Context, manifest, signature []byte, archive io.Reader) (State, error) {
	var header bytes.Buffer
	header.Write(binary.BigEndian.AppendUint32(nil, uint32(len(manifest))))
	header.Write(binary.BigEndian.AppendUint32(nil, uint32(len(signature))))
	return client.request(ctx, http.MethodPost, "stage", io.MultiReader(&header, bytes.NewReader(manifest), bytes.NewReader(signature), archive))
}

func (client *Client) request(ctx context.Context, method, route string, body io.Reader) (State, error) {
	var state State
	if client == nil {
		return state, errors.New("updater unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://updater/"+route, body)
	if err != nil {
		return state, err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return state, err
	}
	defer response.Body.Close()
	err = Decode(io.LimitReader(response.Body, MetadataMax), &state)
	if response.StatusCode != http.StatusOK {
		return state, fmt.Errorf("updater rejected request: %s", state.Error)
	}
	return state, err
}

func decodeStage(reader io.Reader) ([]byte, []byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, nil, err
	}
	rawSize, sigSize := binary.BigEndian.Uint32(header[:4]), binary.BigEndian.Uint32(header[4:])
	if rawSize == 0 || sigSize == 0 || rawSize > MetadataMax || sigSize > MetadataMax {
		return nil, nil, errors.New("update metadata exceeds bounds")
	}
	raw := make([]byte, rawSize)
	sig := make([]byte, sigSize)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return nil, nil, err
	}
	_, err := io.ReadFull(reader, sig)
	return raw, sig, err
}
