package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gnolang/gno/misc/val-scenarios/pkg/valsigner"
	"github.com/gnolang/gno/tm2/pkg/bft/privval"
	"github.com/gnolang/gno/tm2/pkg/crypto/ed25519"
)

func main() {
	var (
		keyFile         = flag.String("key-file", "", "path to the validator private key JSON file (used when --gnokms-addr is empty)")
		control         = flag.String("listen-addr", ":8080", "HTTP control API listen address")
		remoteAddr      = flag.String("remote-signer-addr", "tcp://0.0.0.0:26659", "remote signer listen address")
		gnokmsAddr      = flag.String("gnokms-addr", "", "if set, forward Sign requests to this upstream gnokms remote signer instead of using --key-file")
		gnokmsRequestTO = flag.Duration("gnokms-request-timeout", 5*time.Second, "request timeout for the upstream gnokms remote signer")
		gnokmsKeepAlive = flag.Duration("gnokms-keep-alive", 2*time.Second, "TCP keep alive period for the upstream gnokms remote signer")
		gnokmsMaxDial   = flag.Int("gnokms-max-dial-retries", 30, "fail startup if the upstream gnokms is unreachable after this many 1s retries (negative for indefinite)")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	useGnokms := *gnokmsAddr != ""
	if !useGnokms && *keyFile == "" {
		logger.Error("--key-file is required when --gnokms-addr is empty")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := privval.DefaultPrivValidatorConfig()
	cfg.LocalSigner = *keyFile
	cfg.RemoteSigner.ServerAddress = *gnokmsAddr
	cfg.RemoteSigner.RequestTimeout = *gnokmsRequestTO
	cfg.RemoteSigner.KeepAlivePeriod = *gnokmsKeepAlive
	cfg.RemoteSigner.DialRetryInterval = 1 * time.Second
	cfg.RemoteSigner.DialMaxRetries = *gnokmsMaxDial

	innerSigner, err := privval.NewSignerFromConfig(ctx, cfg, ed25519.GenPrivKey(), logger.With("component", "inner-signer"))
	if err != nil {
		logger.Error("unable to build inner signer", "err", err)
		os.Exit(1)
	}

	server, err := valsigner.NewServer(innerSigner, *control, *remoteAddr, logger)
	if err != nil {
		logger.Error("unable to create signer server", "err", err)
		os.Exit(1)
	}

	if err := server.Start(); err != nil {
		logger.Error("unable to start signer server", "err", err)
		os.Exit(1)
	}

	backend := "local"
	if useGnokms {
		backend = "gnokms"
	}
	logger.Info("valsignerd ready", "backend", backend, "remote_signer_addr", *remoteAddr, "control_addr", *control)

	<-ctx.Done()

	if err := server.Stop(); err != nil {
		logger.Error("unable to stop signer server", "err", err)
		os.Exit(1)
	}
}
