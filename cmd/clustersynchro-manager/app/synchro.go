package app

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/term"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/cmd/clustersynchro-manager/app/config"
	"github.com/clusterpedia-io/clusterpedia/cmd/clustersynchro-manager/app/options"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager"
	"github.com/clusterpedia-io/clusterpedia/pkg/version/verflag"
)

func NewClusterSynchroManagerCommand(ctx context.Context) *cobra.Command {
	opts, _ := options.NewClusterSynchroManagerOptions()
	cmd := &cobra.Command{
		Use: "clustersynchro-manager",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			// k8s.io/kubernetes/cmd/kube-controller-manager/app/controllermanager.go

			// silence client-go warnings.
			// clustersynchro-manager generically watches APIs (including deprecated ones),
			// and CI ensures it works properly against matching kube-apiserver versions.
			restclient.SetDefaultWarningHandler(restclient.NoWarnings{})
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()
			cliflag.PrintFlags(cmd.Flags())

			config, err := opts.Config()
			if err != nil {
				return err
			}

			if err := Run(ctx, config); err != nil {
				return err
			}
			return nil
		},
	}

	namedFlagSets := opts.Flags()
	verflag.AddFlags(namedFlagSets.FlagSet("global"))
	globalflag.AddGlobalFlags(namedFlagSets.FlagSet("global"), cmd.Name())

	fs := cmd.Flags()
	for _, f := range namedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, namedFlagSets, cols)
	return cmd
}

func Run(ctx context.Context, c *config.Config) error {
	synchromanager := synchromanager.NewManager(c.Client, c.CRDClient, c.StorageFactory)
	if !c.LeaderElection.LeaderElect {
		synchromanager.Run(1, ctx.Done())
		return nil
	}

	id, err := os.Hostname()
	if err != nil {
		return err
	}
	id = "local"
	//id += "_" + string(uuid.NewUUID())

	rl, err := resourcelock.NewFromKubeconfig(
		c.LeaderElection.ResourceLock,
		c.LeaderElection.ResourceNamespace,
		id,
		resourcelock.ResourceLockConfig{
			Identity:      c.LeaderElection.ResourceName,
			EventRecorder: c.EventRecorder,
		},
		c.Kubeconfig,
		c.LeaderElection.RenewDeadline.Duration,
	)
	if err != nil {
		return fmt.Errorf("failed to create resource lock: %w", err)
	}

	var done chan struct{}
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Name: c.LeaderElection.ResourceName,

		Lock:          rl,
		LeaseDuration: c.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: c.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   c.LeaderElection.RetryPeriod.Duration,

		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				done = make(chan struct{})
				defer close(done)

				stopCh := ctx.Done()
				synchromanager.Run(1, stopCh)
			},
			OnStoppedLeading: func() {
				klog.Info("leaderelection lost")
				if done != nil {
					<-done
				}
			},
		},
	})
	return nil
}
