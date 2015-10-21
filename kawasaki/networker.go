package kawasaki

import (
	"fmt"
	"net"

	"github.com/cloudfoundry-incubator/guardian/kawasaki/subnets"
	"github.com/pivotal-golang/lager"
)

//go:generate counterfeiter . NetnsMgr
type NetnsMgr interface {
	Create(log lager.Logger, handle string) error
	Lookup(log lager.Logger, handle string) (string, error)
	Destroy(log lager.Logger, handle string) error
}

//go:generate counterfeiter . SpecParser
type SpecParser interface {
	Parse(log lager.Logger, spec string) (subnets.SubnetSelector, subnets.IPSelector, error)
}

//go:generate counterfeiter . ConfigCreator
type ConfigCreator interface {
	Create(log lager.Logger, handle string, subnet *net.IPNet, ip net.IP) (NetworkConfig, error)
}

//go:generate counterfeiter . ConfigApplier
type ConfigApplier interface {
	Apply(log lager.Logger, cfg NetworkConfig, nsPath string) error
}

type Networker struct {
	netnsMgr NetnsMgr

	specParser    SpecParser
	subnetPool    subnets.Pool
	configCreator ConfigCreator
	configApplier ConfigApplier
}

func New(netnsMgr NetnsMgr,
	specParser SpecParser,
	subnetPool subnets.Pool,
	configCreator ConfigCreator,
	configApplier ConfigApplier) *Networker {
	return &Networker{
		netnsMgr: netnsMgr,

		specParser:    specParser,
		subnetPool:    subnetPool,
		configCreator: configCreator,
		configApplier: configApplier,
	}
}

// Network configures a network namespace based on the given spec
// and returns the path to it
func (n *Networker) Network(log lager.Logger, handle, spec string) (string, error) {
	log = log.Session("network", lager.Data{
		"handle": handle,
		"spec":   spec,
	})

	log.Info("started")
	defer log.Info("finished")

	subnetReq, ipReq, err := n.specParser.Parse(log, spec)
	if err != nil {
		log.Error("parse-failed", err)
		return "", err
	}

	subnet, ip, err := n.subnetPool.Acquire(log, subnetReq, ipReq)
	if err != nil {
		log.Error("acquire-failed", err)
		return "", err
	}

	config, err := n.configCreator.Create(log, handle, subnet, ip)
	if err != nil {
		log.Error("create-config-failed", err)
		return "", fmt.Errorf("create network config: %s", err)
	}

	err = n.netnsMgr.Create(log, handle)
	if err != nil {
		log.Error("create-netns-failed", err)
		return "", err
	}

	path, err := n.netnsMgr.Lookup(log, handle)
	if err != nil {
		log.Error("lookup-netns-failed", err)
		return "", err
	}

	if err := n.configApplier.Apply(log, config, path); err != nil {
		log.Error("apply-config-failed", err)
		n.destroyOrLog(log, handle)
		return "", err
	}

	return path, nil
}

func (n *Networker) destroyOrLog(log lager.Logger, handle string) {
	if err := n.netnsMgr.Destroy(log, handle); err != nil {
		log.Error("destroy-failed", err)
	}
}