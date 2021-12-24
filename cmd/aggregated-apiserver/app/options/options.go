package options

import (
	"context"
	"fmt"
	"net"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	netutils "k8s.io/utils/net"

	"github.com/karmada-io/karmada/pkg/aggregatedapiserver"
	clusterv1alpha1 "github.com/karmada-io/karmada/pkg/apis/cluster/v1alpha1"
	clientset "github.com/karmada-io/karmada/pkg/generated/clientset/versioned"
	informers "github.com/karmada-io/karmada/pkg/generated/informers/externalversions"
)

const defaultEtcdPathPrefix = "/registry"

// Options contains everything necessary to create and run aggregated-apiserver.
type Options struct {
	RecommendedOptions    *genericoptions.RecommendedOptions
	SharedInformerFactory informers.SharedInformerFactory
}

// NewOptions returns a new Options.
func NewOptions() *Options {
	o := &Options{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			defaultEtcdPathPrefix,
			aggregatedapiserver.Codecs.LegacyCodec(clusterv1alpha1.SchemeGroupVersion)),
	}
	o.RecommendedOptions.Etcd.StorageConfig.EncodeVersioner = runtime.NewMultiGroupVersioner(clusterv1alpha1.SchemeGroupVersion, schema.GroupKind{Group: clusterv1alpha1.GroupName})
	return o
}

// AddFlags adds flags to the specified FlagSet.
func (o *Options) AddFlags(flags *pflag.FlagSet) {
	o.RecommendedOptions.AddFlags(flags)

	utilfeature.DefaultMutableFeatureGate.AddFlag(flags)
}

// Complete fills in fields required to have valid data.
func (o *Options) Complete() error {
	return nil
}

// Validate validates Options.
func (o *Options) Validate() error {
	var errs []error
	errs = append(errs, o.RecommendedOptions.Validate()...)
	return utilerrors.NewAggregate(errs)
}

// Run runs the aggregated-apiserver with options. This should never exit.
func (o *Options) Run(ctx context.Context) error {
	config, err := o.Config()
	if err != nil {
		return err
	}

	server, err := config.Complete().New()
	if err != nil {
		return err
	}

	server.GenericAPIServer.AddPostStartHookOrDie("start-aggregated-server-informers", func(context genericapiserver.PostStartHookContext) error {
		config.GenericConfig.SharedInformerFactory.Start(context.StopCh)
		o.SharedInformerFactory.Start(context.StopCh)
		return nil
	})

	return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
}

// Config returns config for the api server given Options
func (o *Options) Config() (*aggregatedapiserver.Config, error) {
	// TODO have a "real" external address
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	o.RecommendedOptions.Etcd.StorageConfig.Paging = utilfeature.DefaultFeatureGate.Enabled(features.APIListChunking)

	o.RecommendedOptions.ExtraAdmissionInitializers = func(c *genericapiserver.RecommendedConfig) ([]admission.PluginInitializer, error) {
		client, err := clientset.NewForConfig(c.LoopbackClientConfig)
		if err != nil {
			return nil, err
		}
		informerFactory := informers.NewSharedInformerFactory(client, c.LoopbackClientConfig.Timeout)
		o.SharedInformerFactory = informerFactory
		return []admission.PluginInitializer{}, nil
	}

	serverConfig := genericapiserver.NewRecommendedConfig(aggregatedapiserver.Codecs)
	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, err
	}

	config := &aggregatedapiserver.Config{
		GenericConfig: serverConfig,
		ExtraConfig:   aggregatedapiserver.ExtraConfig{},
	}
	return config, nil
}