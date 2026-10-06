package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// keyInfo is one key from gnokey list.
type keyInfo struct {
	Name, Type, Address, PubKey string
}

// keybase is the gnokey binary: the only program that touches keys.
type keybase interface {
	Version(ctx context.Context) (string, error)
	// List returns errUnparsable when the output format is not recognized.
	List(ctx context.Context) ([]keyInfo, error)
	// Sign signs txPath in place, using the terminal for the password or the
	// Ledger. A key that is not in the keybase returns errKeyNotFound.
	Sign(ctx context.Context, txPath, chainID string, accountNumber, sequence uint64, signer string) error
}

var (
	errUnparsable  = errors.New("unrecognized gnokey list output")
	errKeyNotFound = errors.New("key not found in the keybase")
)

type gnokeyBin struct {
	path   string
	home   string // empty: gnokey's default
	stdin  *os.File
	stdout io.Writer
	stderr io.Writer
}

func (g *gnokeyBin) command(ctx context.Context, args ...string) *exec.Cmd {
	if g.home != "" {
		args = append(args[:1:1], append([]string{"-home", g.home}, args[1:]...)...)
	}
	return exec.CommandContext(ctx, g.path, args...)
}

func (g *gnokeyBin) Version(ctx context.Context) (string, error) {
	out, err := g.command(ctx, "version").Output()
	if err != nil {
		return "", fmt.Errorf("%s version: %w", g.path, err)
	}
	return strings.TrimSpace(strings.TrimPrefix(string(out), "gnokey version:")), nil
}

func (g *gnokeyBin) List(ctx context.Context) ([]keyInfo, error) {
	out, err := g.command(ctx, "list").Output()
	if err != nil {
		return nil, fmt.Errorf("%s list: %w", g.path, err)
	}
	return parseList(string(out))
}

// listLine matches printInfos in tm2/pkg/crypto/keys/client/list.go. The name
// is greedy, so a name containing " (" still parses.
var listLine = regexp.MustCompile(`^\d+\. (.*) \((local|ledger|offline|multi)\) - addr: (\S+) pub: (\S+), path: .*$`)

// parseList reads gnokey list, which is meant for humans, not an API.
func parseList(out string) ([]keyInfo, error) {
	var keys []keyInfo
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		m := listLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("%w: %s", errUnparsable, strconv.Quote(line))
		}
		keys = append(keys, keyInfo{Name: m[1], Type: m[2], Address: m[3], PubKey: m[4]})
	}
	return keys, nil
}

func (g *gnokeyBin) Sign(ctx context.Context, txPath, chainID string, accountNumber, sequence uint64, signer string) error {
	cmd := g.command(ctx, "sign",
		"-tx-path", txPath,
		"-chainid", chainID,
		"-account-number", strconv.FormatUint(accountNumber, 10),
		"-account-sequence", strconv.FormatUint(sequence, 10),
		signer)
	// The child inherits the terminal, so its password prompt and the Ledger
	// flow reach the user unchanged. Its stderr is also kept to tell a
	// missing key apart.
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = g.stdin, g.stdout, io.MultiWriter(g.stderr, &stderr)
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "not found") {
			return fmt.Errorf("%w: %s", errKeyNotFound, signer)
		}
		return fmt.Errorf("gnokey sign: %w", err)
	}
	return nil
}
