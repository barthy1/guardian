package iptables

import (
	"fmt"
	"net"
	"os/exec"

	"github.com/pivotal-golang/lager"
)

type InstanceChainCreator struct {
	iptables *IPTablesController
}

func NewInstanceChainCreator(iptables *IPTablesController) *InstanceChainCreator {
	return &InstanceChainCreator{
		iptables: iptables,
	}
}

func (cc *InstanceChainCreator) Create(logger lager.Logger, handle, instanceId, bridgeName string, ip net.IP, network *net.IPNet) error {
	instanceChain := cc.iptables.InstanceChain(instanceId)

	if err := cc.iptables.CreateChain("nat", instanceChain); err != nil {
		return err
	}

	// Bind nat instance chain to nat prerouting chain
	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s --wait --table nat -A %s --jump %s", cc.iptables.BinPath, cc.iptables.preroutingChain, instanceChain))
	if err := cc.iptables.run("create-instance-chains", cmd); err != nil {
		return err
	}

	// Enable NAT for traffic coming from containers
	cmd = exec.Command("sh", "-c", fmt.Sprintf(
		`(%s --wait --table nat -S %s | grep "\-j MASQUERADE\b" | grep -q -F -- "-s %s") || %s --wait --table nat -A %s --source %s ! --destination %s --jump MASQUERADE`,
		cc.iptables.BinPath, cc.iptables.postroutingChain, network.String(), cc.iptables.BinPath, cc.iptables.postroutingChain,
		network.String(), network.String(),
	))
	if err := cc.iptables.run("create-instance-chains", cmd); err != nil {
		return err
	}

	// Create filter instance chain
	if err := cc.iptables.CreateChain("filter", instanceChain); err != nil {
		return err
	}

	// Allow intra-subnet traffic (Linux ethernet bridging goes through ip stack)
	cmd = exec.Command("sh", "-c", fmt.Sprintf("%s --wait -A %s -s %s -d %s -j ACCEPT", cc.iptables.BinPath, instanceChain, network.String(), network.String()))
	if err := cc.iptables.run("create-instance-chains", cmd); err != nil {
		return err
	}

	// Otherwise, use the default filter chain
	cmd = exec.Command("sh", "-c", fmt.Sprintf("%s --wait -A %s --goto %s", cc.iptables.BinPath, instanceChain, cc.iptables.defaultChain))
	if err := cc.iptables.run("create-instance-chains", cmd); err != nil {
		return err
	}

	// Bind filter instance chain to filter forward chain
	cmd = exec.Command("sh", "-c", fmt.Sprintf("%s --wait -I %s 2 --in-interface %s --source %s --goto %s", cc.iptables.BinPath, cc.iptables.forwardChain, bridgeName, ip.String(), instanceChain))
	if err := cc.iptables.run("create-instance-chains", cmd); err != nil {
		return err
	}

	// Create Logging Chain
	return cc.createLoggingChain(logger, handle, instanceId)
}

func (cc *InstanceChainCreator) createLoggingChain(logger lager.Logger, handle, instanceId string) error {
	instanceChain := cc.iptables.InstanceChain(instanceId)
	loggingChain := fmt.Sprintf("%s-log", instanceChain)

	if err := cc.iptables.CreateChain("filter", loggingChain); err != nil {
		return err
	}

	if len(handle) > 29 {
		handle = handle[0:29]
	}

	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s --wait -A %s -m conntrack --ctstate NEW,UNTRACKED,INVALID --protocol tcp --jump LOG --log-prefix %s", cc.iptables.BinPath, loggingChain, handle))
	if err := cc.iptables.run("create-instance-chains", cmd); err != nil {
		return err
	}

	cmd = exec.Command("sh", "-c", fmt.Sprintf("%s --wait -A %s --jump RETURN", cc.iptables.BinPath, loggingChain))
	if err := cc.iptables.run("create-instance-chains", cmd); err != nil {
		return err
	}

	return nil
}

func (cc *InstanceChainCreator) Destroy(logger lager.Logger, instanceId string) error {
	instanceChain := cc.iptables.InstanceChain(instanceId)

	// Prune nat prerouting chain
	cmd := exec.Command("sh", "-c", fmt.Sprintf(
		`%s --wait --table nat -S %s 2> /dev/null | grep "\-j %s\b" | sed -e "s/-A/-D/" | xargs --no-run-if-empty --max-lines=1 %s --wait --table nat`,
		cc.iptables.BinPath, cc.iptables.preroutingChain, instanceChain, cc.iptables.BinPath,
	))
	if err := cc.iptables.run("prune-prerouting-chain", cmd); err != nil {
		return err
	}

	// Flush instance chain
	if err := cc.iptables.FlushChain("nat", instanceChain); err != nil {
		return err
	}

	// Delete nat instance chain
	if err := cc.iptables.DeleteChain("nat", instanceChain); err != nil {
		return err
	}

	// Prune forward chain
	cmd = exec.Command("sh", "-c", fmt.Sprintf(
		`%s --wait -S %s 2> /dev/null | grep "\-g %s\b" | sed -e "s/-A/-D/" | xargs --no-run-if-empty --max-lines=1 %s --wait`,
		cc.iptables.BinPath, cc.iptables.forwardChain, instanceChain, cc.iptables.BinPath,
	))
	if err := cc.iptables.run("prune-forward-chain", cmd); err != nil {
		return err
	}

	// Flush instance chain
	cc.iptables.FlushChain("filter", instanceChain)

	// delete instance chain
	cc.iptables.DeleteChain("filter", instanceChain)

	// delete the logging chain
	instanceLoggingChain := fmt.Sprintf("%s-log", instanceChain)
	cc.iptables.FlushChain("filter", instanceLoggingChain)
	cc.iptables.DeleteChain("filter", instanceLoggingChain)

	return nil
}
