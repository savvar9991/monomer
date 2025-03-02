package e2e

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	bftclient "github.com/cometbft/cometbft/rpc/client/http"
	opbindings "github.com/ethereum-optimism/optimism/op-bindings/bindings"
	opgenesis "github.com/ethereum-optimism/optimism/op-chain-ops/genesis"
	"github.com/ethereum-optimism/optimism/op-node/bindings"
	"github.com/ethereum-optimism/optimism/op-node/rollup"
	"github.com/polymerdao/monomer/environment"
)

type StackConfig struct {
	Ctx                  context.Context
	Users                []*ecdsa.PrivateKey
	L1Client             *L1Client
	L1Deployments        *opgenesis.L1Deployments
	OptimismPortal       *bindings.OptimismPortal
	L1StandardBridge     *opbindings.L1StandardBridge
	L2OutputOracleCaller *bindings.L2OutputOracleCaller
	L2Client             *bftclient.HTTP
	MonomerClient        *MonomerClient
	RollupConfig         *rollup.Config
	WaitL1               func(numBlocks int) error
	WaitL2               func(numBlocks int) error
}

func Run(
	ctx context.Context,
	env *environment.Env,
	outDir string,
) error {
	monomerPath, err := filepath.Abs("..")
	if err != nil {
		return fmt.Errorf("absolute path to local monomer directory: %v", err)
	}
	const appName = "e2eapp"
	appDirPath := filepath.Join(outDir, appName)
	//nolint:gosec // We aren't worried about tainted cmd args.
	monogenCmd := setupCmd(exec.CommandContext(ctx,
		filepath.Join("..", "scripts", "go-wrapper.sh"),
		"run", filepath.Join("..", "cmd", "monogen"),
		"--app-dir-path", appDirPath,
		"--gomod-path", "github.com/e2e/"+appName,
		"--address-prefix", "e2e",
		"--monomer-path", monomerPath,
	))
	if err := monogenCmd.Run(); err != nil {
		return fmt.Errorf("run monogen: %v", err)
	}

	setupHelperCmd := setupCmd(exec.CommandContext(ctx, filepath.Join(appDirPath, "setup-helper.sh"))) //nolint:gosec
	setupHelperCmd.Dir = appDirPath
	// Add the "GOFLAGS='-gcflags=all=-N -l'" environment variable to disable optimizations and make debugging easier.
	setupHelperCmd.Env = append(os.Environ(), "e2eapp_HOME="+outDir)
	if err := setupHelperCmd.Run(); err != nil {
		return fmt.Errorf("run setup helper: %v", err)
	}

	//nolint:gosec // We aren't worried about tainted cmd args.
	appCmd := setupCmd(exec.CommandContext(ctx,
		filepath.Join(appDirPath, appName+"d"),
		"monomer",
		"start",
		"--minimum-gas-prices", "0.001wei",
		"--monomer.dev-start",
	))
	appCmd.Dir = appDirPath
	appCmd.Env = append(os.Environ(), "e2eapp_HOME="+outDir)
	if err := appCmd.Start(); err != nil {
		return fmt.Errorf("run app: %v", err)
	}
	env.DeferErr("wait for app", func() error {
		err := appCmd.Wait()
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	})

	return nil
}

func setupCmd(cmd *exec.Cmd) *exec.Cmd {
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
