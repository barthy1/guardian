package iptables_test

import (
	"fmt"
	"os/exec"

	"github.com/cloudfoundry-incubator/guardian/kawasaki/iptables"
	fakes "github.com/cloudfoundry-incubator/guardian/kawasaki/iptables/iptablesfakes"
	"github.com/cloudfoundry/gunk/command_runner/fake_command_runner"
	. "github.com/cloudfoundry/gunk/command_runner/fake_command_runner/matchers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("IPTables controller", func() {
	var (
		netnsName          string
		prefix             string
		iptablesBin        string
		fakeRunner         *fake_command_runner.FakeCommandRunner
		iptablesController iptables.IPTables
	)

	BeforeEach(func() {
		netnsName = fmt.Sprintf("ginkgo-netns-%d", GinkgoParallelNode())
		makeNamespace(netnsName)

		fakeRunner = fake_command_runner.New()
		fakeRunner.WhenRunning(fake_command_runner.CommandSpec{},
			func(cmd *exec.Cmd) error {
				return wrapCmdInNs(netnsName, cmd).Run()
			},
		)

		prefix = fmt.Sprintf("g-%d", GinkgoParallelNode())
		iptablesBin = "/sbin/iptables"
		iptablesController = iptables.New(iptablesBin, fakeRunner, prefix)
	})

	AfterEach(func() {
		deleteNamespace(netnsName)
	})

	Describe("Using the binary provided", func() {
		BeforeEach(func() {
			iptablesBin = "/dummy/spiderman/path"
			iptablesController = iptables.New(iptablesBin, fakeRunner, prefix)
		})

		It("should use the binary", func() {
			iptablesController.CreateChain("filter", "test-chain")

			cmd := fake_command_runner.CommandSpec{
				Path: "sh",
				Args: []string{"-c", fmt.Sprintf("%s --wait --table filter -N test-chain", iptablesBin)},
			}

			Expect(fakeRunner).To(HaveExecutedSerially(cmd))
		})
	})

	Describe("CreateChain", func() {
		It("creates the chain", func() {
			Expect(iptablesController.CreateChain("filter", "test-chain")).To(Succeed())

			sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-L", "test-chain")), GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sess).Should(gexec.Exit(0))
		})

		Context("when the table is nat", func() {
			It("creates the nat chain", func() {
				Expect(iptablesController.CreateChain("nat", "test-chain")).To(Succeed())

				sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-t", "nat", "-L", "test-chain")), GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sess).Should(gexec.Exit(0))
			})
		})

		Context("when the chain exists", func() {
			BeforeEach(func() {
				Expect(iptablesController.CreateChain("nat", "test-chain")).To(Succeed())
			})

			It("returns an error", func() {
				Expect(iptablesController.CreateChain("nat", "test-chain")).NotTo(Succeed())
			})
		})
	})

	Describe("PrependRule", func() {
		It("prepends the rule", func() {
			fakeTCPRule := new(fakes.FakeRule)
			fakeTCPRule.FlagsReturns([]string{"--protocol", "tcp"})
			fakeUDPRule := new(fakes.FakeRule)
			fakeUDPRule.FlagsReturns([]string{"--protocol", "udp"})

			Expect(iptablesController.CreateChain("filter", "test-chain")).To(Succeed())

			Expect(iptablesController.PrependRule("test-chain", fakeTCPRule)).To(Succeed())
			Expect(iptablesController.PrependRule("test-chain", fakeUDPRule)).To(Succeed())

			buff := gbytes.NewBuffer()
			sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-S", "test-chain")), buff, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sess).Should(gexec.Exit(0))
			Expect(buff).To(gbytes.Say("-A test-chain -p udp\n-A test-chain -p tcp"))
		})

		It("returns an error when the chain does not exist", func() {
			fakeRule := new(fakes.FakeRule)
			fakeRule.FlagsReturns([]string{})

			Expect(iptablesController.PrependRule("test-chain", fakeRule)).NotTo(Succeed())
		})
	})

	Describe("DeleteChain", func() {
		BeforeEach(func() {
			Expect(iptablesController.CreateChain("filter", "test-chain")).To(Succeed())
		})

		It("deletes the chain", func() {
			Expect(iptablesController.DeleteChain("filter", "test-chain")).To(Succeed())

			sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-L", "test-chain")), GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sess).Should(gexec.Exit(1))
		})

		Context("when the table is nat", func() {
			BeforeEach(func() {
				Expect(iptablesController.CreateChain("nat", "test-chain")).To(Succeed())
			})

			It("deletes the nat chain", func() {
				Expect(iptablesController.DeleteChain("nat", "test-chain")).To(Succeed())

				sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-t", "nat", "-L", "test-chain")), GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sess).Should(gexec.Exit(1))
			})
		})

		Context("when the chain does not exist", func() {
			It("does not return an error", func() {
				Expect(iptablesController.DeleteChain("filter", "test-non-existing-chain")).To(Succeed())
			})
		})
	})

	Describe("FlushChain", func() {
		var table string

		BeforeEach(func() {
			table = "filter"
		})

		JustBeforeEach(func() {
			Expect(iptablesController.CreateChain(table, "test-chain")).To(Succeed())

			sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-t", table, "-A", "test-chain", "-j", "ACCEPT")), GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sess).Should(gexec.Exit(0))
		})

		It("flushes the chain", func() {
			Expect(iptablesController.FlushChain(table, "test-chain")).To(Succeed())

			buff := gbytes.NewBuffer()
			sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-t", table, "-S", "test-chain")), buff, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sess).Should(gexec.Exit(0))
			Consistently(buff).ShouldNot(gbytes.Say("-A test-chain"))
		})

		Context("when the table is nat", func() {
			BeforeEach(func() {
				table = "nat"
			})

			It("flushes the nat chain", func() {
				Expect(iptablesController.FlushChain(table, "test-chain")).To(Succeed())

				buff := gbytes.NewBuffer()
				sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-t", table, "-S", "test-chain")), buff, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sess).Should(gexec.Exit(0))
				Consistently(buff).ShouldNot(gbytes.Say("-A test-chain"))
			})
		})

		Context("when the chain does not exist", func() {
			It("does not return an error", func() {
				Expect(iptablesController.FlushChain("filter", "test-non-existing-chain")).To(Succeed())
			})
		})
	})

	Describe("DeleteChainReferences", func() {
		var table string

		BeforeEach(func() {
			table = "filter"
		})

		JustBeforeEach(func() {
			Expect(iptablesController.CreateChain(table, "test-chain-1")).To(Succeed())
			Expect(iptablesController.CreateChain(table, "test-chain-2")).To(Succeed())

			sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-t", table, "-A", "test-chain-1", "-j", "test-chain-2")), GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())
			Eventually(sess).Should(gexec.Exit(0))
		})

		It("deletes the references", func() {
			Expect(iptablesController.DeleteChainReferences(table, "test-chain-1", "test-chain-2")).To(Succeed())

			Eventually(func() string {
				buff := gbytes.NewBuffer()
				sess, err := gexec.Start(wrapCmdInNs(netnsName, exec.Command(iptablesBin, "-t", table, "-S", "test-chain-1")), buff, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())

				Eventually(sess).Should(gexec.Exit(0))

				return string(buff.Contents())
			}).ShouldNot(ContainSubstring("test-chain-2"))
		})
	})
})

func makeNamespace(nsName string) {
	sess, err := gexec.Start(exec.Command("ip", "netns", "add", nsName), GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(sess).Should(gexec.Exit(0))
}

func deleteNamespace(nsName string) {
	sess, err := gexec.Start(exec.Command("ip", "netns", "delete", nsName), GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(sess).Should(gexec.Exit(0))
}

func wrapCmdInNs(nsName string, cmd *exec.Cmd) *exec.Cmd {
	wrappedCmd := exec.Command("ip", "netns", "exec", nsName)
	wrappedCmd.Args = append(wrappedCmd.Args, cmd.Args...)
	wrappedCmd.Stdout = cmd.Stdout
	wrappedCmd.Stderr = cmd.Stderr
	return wrappedCmd
}
