package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/update"
)

func main() {
	input := flag.String("input", "module-builds", "directory of tested build artifacts")
	output := flag.String("output", "module-release", "GitHub release asset directory")
	sequence := flag.Uint64("sequence", 0, "increasing catalog sequence number")
	metadata := flag.String("metadata", "", "JSON module definitions and versions for -prepare or a local build fixture")
	testFixture := flag.Bool("test-fixture", false, "generate a local feed and ephemeral test keypair")
	prepare := flag.Bool("prepare", false, "write an unsigned release candidate and content-addressed assets")
	candidate := flag.Bool("candidate", false, "read candidate.json and previously prepared assets from input")
	flag.Parse()
	if *prepare && (*testFixture || *candidate) || !*prepare && !*testFixture && !*candidate || *candidate && *metadata != "" {
		log.Fatal("choose -prepare, -test-fixture (optionally -candidate), or -candidate for production signing")
	}
	if !*candidate && *metadata == "" {
		log.Fatal("-metadata is required with build artifacts")
	}
	now := time.Now().UTC().Truncate(time.Second)
	var modules []moduleDefinition
	var err error
	if !*candidate {
		modules, err = loadDefinitions(*metadata)
		if err != nil {
			log.Fatal(err)
		}
	}
	if *prepare {
		catalog := newCatalog(1, now, false)
		files, err := assembleCatalog(&catalog, *input, modules, now, false)
		if err == nil {
			var data []byte
			data, err = json.Marshal(releaseCandidate{Modules: catalog.Modules, Assets: catalog.Assets})
			if err == nil {
				err = writeFiles(*output, files, "candidate.json", data)
			}
		}
		if err != nil {
			log.Fatal(err)
		}
		return
	} else if *testFixture {
		err = writeTestFixture(*input, *output, *sequence, now, modules)
	} else {
		err = signCatalog(*input, *output, *sequence, now)
	}
	if err != nil {
		log.Fatal(err)
	}
}

type moduleDefinition struct {
	nginxmodule.Module
	Version string `json:"version"`
}

func loadDefinitions(path string) ([]moduleDefinition, error) {
	var definitions []moduleDefinition
	data, err := readBuildFile(path, nginxmodule.MaxEnvelope)
	if err != nil {
		return nil, err
	}
	if err := update.Decode(bytes.NewReader(data), &definitions); err != nil || definitions == nil {
		return nil, fmt.Errorf("invalid module metadata array")
	}
	return definitions, nil
}

func signCatalog(input, output string, sequence uint64, now time.Time) error {
	trust, err := nginxmodule.Keys(os.Getenv("NXPANEL_MODULE_TRUST_KEYS"), os.Getenv("NXPANEL_UPDATE_TRUST_KEYS"))
	if err != nil {
		return err
	}
	keys := make(map[string]ed25519.PrivateKey)
	for _, entry := range strings.Split(os.Getenv("NXPANEL_MODULE_SIGNING_KEYS"), ",") {
		id, encoded, ok := strings.Cut(entry, ":")
		seed, err := base64.StdEncoding.DecodeString(encoded)
		if !ok || err != nil || len(seed) != ed25519.SeedSize || keys[id] != nil {
			return fmt.Errorf("invalid module signing secret")
		}
		key := ed25519.NewKeyFromSeed(seed)
		if !key.Public().(ed25519.PublicKey).Equal(trust[id]) {
			return fmt.Errorf("module signing secret does not match configured trust keys")
		}
		keys[id] = key
	}
	return writeCatalog(input, output, sequence, now, keys, trust, false, nil)
}

func writeTestFixture(input, output string, sequence uint64, now time.Time, definitions []moduleDefinition) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	const id = "module-fixture"
	if err := os.Mkdir(output, 0700); err != nil {
		return err
	}
	if err := writeCatalog(input, filepath.Join(output, "feed"), sequence, now, map[string]ed25519.PrivateKey{id: private}, map[string]ed25519.PublicKey{id: public}, true, definitions); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "trust.pub"), []byte(id+":"+base64.StdEncoding.EncodeToString(public)+"\n"), 0644)
}

type releaseCandidate struct {
	Modules []nginxmodule.Module `json:"modules"`
	Assets  []nginxmodule.Asset  `json:"assets"`
}

func newCatalog(sequence uint64, now time.Time, test bool) nginxmodule.Catalog {
	product := nginxmodule.Product
	validity := nginxmodule.MaxValidity
	if test {
		product = nginxmodule.TestProduct
		validity = 24 * time.Hour
	}
	return nginxmodule.Catalog{Product: product, Protocol: nginxmodule.Protocol, Sequence: sequence, IssuedAt: now, ExpiresAt: now.Add(validity), Modules: []nginxmodule.Module{}, Assets: []nginxmodule.Asset{}}
}

func assembleCatalog(catalog *nginxmodule.Catalog, input string, definitions []moduleDefinition, now time.Time, test bool) (map[string][]byte, error) {
	validate := catalog.Validate
	if test {
		validate = catalog.ValidateTest
	}
	builds := make(map[string][]byte)
	for _, definition := range definitions {
		if !nginxmodule.ValidID(definition.ID) || definition.Kind == nginxmodule.DynamicKind && !nginxmodule.ValidSymbol(definition.Symbol) {
			return nil, fmt.Errorf("invalid module ID or symbol")
		}
		loadScript := func(action string) (nginxmodule.Script, error) {
			body, err := readBuildFile(filepath.Join(input, definition.ID, action+".sh"), nginxmodule.MaxScript)
			if err != nil {
				return nginxmodule.Script{}, err
			}
			if len(body) == 0 || bytes.ContainsRune(body, 0) {
				return nginxmodule.Script{}, fmt.Errorf("invalid module shell script")
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(body))
			name := nginxmodule.ScriptName(definition.ID, action, digest)
			builds[name] = body
			return nginxmodule.Script{Name: name, Size: int64(len(body)), SHA256: digest}, nil
		}
		var err error
		if definition.Kind == nginxmodule.DynamicKind {
			if definition.Install, err = loadScript("install"); err != nil {
				return nil, err
			}
			if definition.Uninstall, err = loadScript("uninstall"); err != nil {
				return nil, err
			}
		}
		catalog.Modules = append(catalog.Modules, definition.Module)
	}
	if err := validate(now); err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		if definition.Kind != nginxmodule.DynamicKind {
			continue
		}
		dir := filepath.Join(input, definition.ID)
		paths, err := filepath.Glob(filepath.Join(dir, "*", "target.json"))
		if err != nil || len(catalog.Assets)+len(paths) > 128 {
			return nil, fmt.Errorf("too many tested build artifacts or invalid input path")
		}
		for _, path := range paths {
			target, err := readBuildTarget(path)
			if err != nil {
				return nil, err
			}
			body, err := readBuildFile(filepath.Join(filepath.Dir(path), definition.Symbol+".so"), nginxmodule.MaxAsset)
			if err != nil {
				return nil, err
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(body))
			name := nginxmodule.AssetName(definition.ID, digest)
			builds[name] = body
			catalog.Assets = append(catalog.Assets, nginxmodule.Asset{
				Module: definition.ID, Version: definition.Version, Target: target, Name: name, SHA256: digest, Size: int64(len(body)),
			})
		}
	}
	return builds, validate(now)
}

func readCandidate(input string, catalog *nginxmodule.Catalog, now time.Time, test bool) (map[string][]byte, error) {
	data, err := readBuildFile(filepath.Join(input, "candidate.json"), nginxmodule.MaxEnvelope)
	if err != nil {
		return nil, err
	}
	var candidate releaseCandidate
	if err := update.Decode(bytes.NewReader(data), &candidate); err != nil {
		return nil, err
	}
	catalog.Modules, catalog.Assets = candidate.Modules, candidate.Assets
	production := newCatalog(catalog.Sequence, now, false)
	production.Modules, production.Assets = candidate.Modules, candidate.Assets
	if err := production.Validate(now); err != nil {
		return nil, err
	}
	builds := make(map[string][]byte)
	load := func(name string, size int64, digest string) error {
		body, err := readBuildFile(filepath.Join(input, name), size)
		if err != nil {
			return err
		}
		if int64(len(body)) != size || fmt.Sprintf("%x", sha256.Sum256(body)) != digest {
			return fmt.Errorf("candidate asset hash mismatch: %s", name)
		}
		builds[name] = body
		return nil
	}
	for index, module := range catalog.Modules {
		if test {
			catalog.Modules[index].Release = nginxmodule.Release{}
		}
		if module.Kind == nginxmodule.DynamicKind {
			for _, script := range []nginxmodule.Script{module.Install, module.Uninstall} {
				if err := load(script.Name, script.Size, script.SHA256); err != nil {
					return nil, err
				}
				if bytes.ContainsRune(builds[script.Name], 0) {
					return nil, fmt.Errorf("invalid module shell script: %s", script.Name)
				}
			}
		}
	}
	for _, asset := range catalog.Assets {
		if err := load(asset.Name, asset.Size, asset.SHA256); err != nil {
			return nil, err
		}
	}
	return builds, nil
}

func writeCatalog(input, output string, sequence uint64, now time.Time, keys map[string]ed25519.PrivateKey, trust map[string]ed25519.PublicKey, test bool, definitions []moduleDefinition) error {
	purpose, validate, verify := nginxmodule.Purpose, nginxmodule.Catalog.Validate, nginxmodule.Verify
	if test {
		purpose, validate, verify = nginxmodule.TestPurpose, nginxmodule.Catalog.ValidateTest, nginxmodule.VerifyTest
	}
	catalog := newCatalog(sequence, now, test)
	var builds map[string][]byte
	var err error
	if definitions == nil {
		builds, err = readCandidate(input, &catalog, now, test)
	} else {
		builds, err = assembleCatalog(&catalog, input, definitions, now, test)
	}
	if err != nil {
		return err
	}
	if err := validate(catalog, now); err != nil {
		return err
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	envelope := nginxmodule.Envelope{Catalog: raw, Signatures: make(map[string]string)}
	message := update.SignatureMessage(purpose, raw)
	for id, key := range keys {
		envelope.Signatures[id] = base64.StdEncoding.EncodeToString(ed25519.Sign(key, message))
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if _, _, err := verify(data, trust, now, nginxmodule.Floor{}); err != nil {
		return err
	}
	return writeFiles(output, builds, nginxmodule.CatalogName, data)
}

func writeFiles(output string, builds map[string][]byte, manifest string, data []byte) error {
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	for name, body := range builds {
		if err := os.WriteFile(filepath.Join(output, name), body, 0644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(output, manifest), data, 0644)
}

func readBuildTarget(path string) (nginxmodule.Target, error) {
	var target nginxmodule.Target
	data, err := readBuildFile(path, 1024)
	if err != nil {
		return target, err
	}
	return target, update.Decode(bytes.NewReader(data), &target)
}

func readBuildFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		return nil, fmt.Errorf("build artifact exceeds size limit: %s", path)
	}
	return data, err
}
