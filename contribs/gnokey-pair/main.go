// gnokey-pair receives a transaction from a phone over an end-to-end
// encrypted magic-wormhole channel, shows its own review, and has the
// unmodified gnokey binary sign it. See
// docs/superpowers/specs/2026-10-05-gnokey-pair-design.md.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/gnolang/gno/tm2/pkg/commands"
)

func main() {
	cfg := &config{}
	cmd := commands.NewCommand(
		commands.Metadata{
			Name:       "gnokey-pair",
			ShortUsage: "gnokey-pair [flags]",
			ShortHelp:  "sign on this computer a transaction sent from your phone",
			LongHelp: "gnokey-pair shows a code and a QR for Gnokey Mobile's \"Send to my computer\".\n" +
				"It receives one unsigned transaction over an end-to-end encrypted channel,\n" +
				"reviews it, signs it with gnokey, and broadcasts it or returns it to the phone.",
		},
		cfg,
		func(ctx context.Context, args []string) error {
			if len(args) != 0 {
				return flag.ErrHelp
			}
			return execPair(ctx, *cfg)
		},
	)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// A second Ctrl-C quits at once.
	go func() {
		<-ctx.Done()
		stop()
	}()
	cmd.Execute(ctx, os.Args[1:])
}

func (c *config) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.remote, "remote", "", "node for the request's chain (default: ask before using the one the phone suggests)")
	fs.StringVar(&c.relay, "relay", DefaultRelay, "mailbox server")
	fs.StringVar(&c.gnokey, "gnokey", "gnokey", "gnokey binary, looked up in $PATH")
	fs.StringVar(&c.home, "home", "", "passed to gnokey as -home")
	fs.DurationVar(&c.timeout, "timeout", 10*time.Minute, "how long to wait for the phone")
	fs.DurationVar(&c.linger, "linger", 10*time.Minute, "after answering, how long to wait for the phone to read the answer")
}

func execPair(ctx context.Context, cfg config) error {
	path, err := exec.LookPath(cfg.gnokey)
	if err != nil {
		return err
	}
	cfg.gnokey = path
	p := &pairing{
		cfg:  cfg,
		in:   bufio.NewReader(os.Stdin),
		out:  os.Stdout,
		keys: &gnokeyBin{path: path, home: cfg.home, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr},
		dial: dialNode,
		now:  time.Now,
	}
	err = p.run(ctx)
	if errors.Is(err, errCancelled) {
		fmt.Fprintln(os.Stderr, "cancelled")
		os.Exit(1)
	}
	return err
}
