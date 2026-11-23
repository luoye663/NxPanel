package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/luoye663/nxpanel/internal/trust"
)

func main() {
	input := flag.String("sign", "", "path to reviewed policy JSON to sign offline")
	check := flag.String("check", "", "directory containing a signed policy to verify")
	output := flag.String("out", "", "new directory for the signed policy")
	key := flag.String("key", "", "path to the offline root signing seed")
	previous := flag.String("previous", "", "directory containing the previous signed policy")
	flag.Parse()
	root, now := os.Getenv("NXPANEL_ROOT_PUBLIC_KEY"), time.Now()
	var signed trust.Signed
	var err error
	switch {
	case *input != "" && *check == "" && *output != "" && *key != "":
		signed, err = sign(*input, *key, root, now)
	case *check != "" && *input == "" && *output == "" && *key == "":
		signed, err = trust.ReadDir(*check)
	default:
		log.Fatal("use -sign FILE -key FILE -out NEW_DIR or -check DIR")
	}
	if err != nil {
		log.Fatal(err)
	}
	policy, next, err := signed.Verify(root, trust.Floor{}, now)
	if err == nil {
		err = policy.RequireCurrent(now)
	}
	if err == nil && *previous != "" {
		previousRoot := os.Getenv("NXPANEL_PREVIOUS_ROOT_PUBLIC_KEY")
		if previousRoot == "" {
			previousRoot = root
		}
		err = checkSequence(*previous, previousRoot, now, next.Sequence)
	}
	if err != nil {
		log.Fatal(err)
	}
	if *input != "" {
		if err := writePolicy(*output, signed); err != nil {
			log.Fatal(err)
		}
	}
}

func sign(input, key, root string, now time.Time) (trust.Signed, error) {
	raw, err := trust.ReadFile(input, trust.MaxPolicy)
	if err != nil {
		return trust.Signed{}, err
	}
	seed, err := trust.ReadFile(key, trust.MaxSig)
	if err != nil {
		return trust.Signed{}, err
	}
	_, private, err := trust.AuthorizedPrivate(string(seed), root)
	if err != nil {
		return trust.Signed{}, err
	}
	raw, err = trust.WithDefaultValidity(raw, now)
	if err != nil {
		return trust.Signed{}, err
	}
	return trust.Sign(raw, private)
}

func checkSequence(dir, root string, now time.Time, sequence uint64) error {
	signed, err := trust.ReadDir(dir)
	if err != nil {
		return err
	}
	_, previous, err := signed.Verify(root, trust.Floor{}, now)
	if err != nil {
		return err
	}
	if sequence <= previous.Sequence {
		return errors.New("new trust policy must increase the sequence")
	}
	return nil
}

func writePolicy(dir string, signed trust.Signed) error {
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, trust.Name), signed.Policy, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, trust.Signature), signed.Signature, 0644)
}
