// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package networkcmd

import (
	"fmt"
	"path"

	"golang.org/x/mod/semver"

	"github.com/ava-labs/avalanche-cli/pkg/binutils"
	"github.com/ava-labs/avalanche-cli/pkg/constants"
	"github.com/ava-labs/avalanche-cli/pkg/subnet"
	"github.com/ava-labs/avalanche-cli/pkg/ux"
	"github.com/ava-labs/avalanche-network-runner/client"
	"github.com/ava-labs/avalanche-network-runner/server"
	"github.com/ava-labs/avalanche-network-runner/utils"
	"github.com/spf13/cobra"
)

var (
	avagoVersion string
	snapshotName string
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a local network",
		Long: `The network start command starts a local, multi-node Avalanche network on your machine.

By default, the command loads the default snapshot. If you provide the --snapshot-name
flag, the network loads that snapshot instead. The command fails if the local network is
already running.`,

		RunE:         startNetwork,
		Args:         cobra.ExactArgs(0),
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&avagoVersion, "avalanchego-version", "latest", "use this version of avalanchego (ex: v1.17.12)")
	cmd.Flags().StringVar(&snapshotName, "snapshot-name", constants.DefaultSnapshotName, "name of snapshot to use to start the network from")

	return cmd
}

func startNetwork(*cobra.Command, []string) error {
	sd := subnet.NewLocalDeployer(app, avagoVersion, "")

	if err := sd.StartServer(); err != nil {
		return err
	}

	avagoVersion, avalancheGoBinPath, pluginDir, err := sd.SetupLocalEnv()
	if err != nil {
		return err
	}

	cli, err := binutils.NewGRPCClient()
	if err != nil {
		return err
	}

	var startMsg string
	if snapshotName == constants.DefaultSnapshotName {
		startMsg = "Starting previously deployed and stopped snapshot"
	} else {
		startMsg = fmt.Sprintf("Starting previously deployed and stopped snapshot %s...", snapshotName)
	}
	ux.Logger.PrintToUser(startMsg)

	outputDirPrefix := path.Join(app.GetRunDir(), "restart")
	outputDir, err := utils.MkDirWithTimestamp(outputDirPrefix)
	if err != nil {
		return err
	}

	loadSnapshotOpts := []client.OpOption{
		client.WithExecPath(avalancheGoBinPath),
		client.WithRootDataDir(outputDir),
		client.WithReassignPortsIfUsed(true),
	}

	// For avago version < AvalancheGoPluginDirFlagAdded, we use ANR default location for plugins dir,
	// for >= AvalancheGoPluginDirFlagAdded, we pass the param
	// TODO: review this once ANR includes proper avago version management
	if semver.Compare(avagoVersion, constants.AvalancheGoPluginDirFlagAdded) >= 0 {
		loadSnapshotOpts = append(loadSnapshotOpts, client.WithPluginDir(pluginDir))
	}

	// load global node configs if they exist
	configStr, err := app.Conf.LoadNodeConfig()
	if err != nil {
		return err
	}
	if configStr != "" {
		loadSnapshotOpts = append(loadSnapshotOpts, client.WithGlobalNodeConfig(configStr))
	}

	ctx := binutils.GetAsyncContext()

	_, err = cli.LoadSnapshot(
		ctx,
		snapshotName,
		loadSnapshotOpts...,
	)

	if err != nil {
		if !server.IsServerError(err, server.ErrAlreadyBootstrapped) {
			return fmt.Errorf("failed to start network with the persisted snapshot: %w", err)
		}
		ux.Logger.PrintToUser("Network has already been booted. Wait until healthy...")
	} else {
		ux.Logger.PrintToUser("Booting Network. Wait until healthy...")
	}

	// TODO: this should probably be extracted from the deployer and
	// used as an independent helper
	clusterInfo, err := sd.WaitForHealthy(ctx, cli, constants.HealthCheckInterval)
	if err != nil {
		return fmt.Errorf("failed waiting for network to become healthy: %w", err)
	}

	endpoints := subnet.GetEndpoints(clusterInfo)

	fmt.Println()
	if len(endpoints) > 0 {
		ux.Logger.PrintToUser("Network ready to use. Local network node endpoints:")
		ux.PrintTableEndpoints(clusterInfo)
	}

	return nil
}
