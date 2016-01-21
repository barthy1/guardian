package kawasaki_test

import (
	"net"

	"github.com/cloudfoundry-incubator/guardian/kawasaki"
	"github.com/cloudfoundry-incubator/guardian/kawasaki/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"
)

var _ = Describe("ConfigCreator", func() {
	var (
		creator     *kawasaki.Creator
		subnet      *net.IPNet
		ip          net.IP
		externalIP  net.IP
		logger      lager.Logger
		idGenerator *fakes.FakeIDGenerator
	)

	BeforeEach(func() {
		var err error
		ip, subnet, err = net.ParseCIDR("192.168.12.20/24")
		Expect(err).NotTo(HaveOccurred())

		externalIP = net.ParseIP("220.10.120.5")

		logger = lagertest.NewTestLogger("test")
		idGenerator = &fakes.FakeIDGenerator{}

		creator = kawasaki.NewConfigCreator(idGenerator, "w1", "0123456789abcdef", externalIP)
	})

	It("panics if the interface prefix is longer than 2 characters", func() {
		Expect(func() {
			kawasaki.NewConfigCreator(idGenerator, "too-long", "wc", externalIP)
		}).To(Panic())
	})

	It("panics if the chain prefix is longer than 16 characters", func() {
		Expect(func() {
			kawasaki.NewConfigCreator(idGenerator, "w1", "0123456789abcdefg", externalIP)
		}).To(Panic())
	})

	It("assigns the bridge name based on the subnet", func() {
		config, err := creator.Create(logger, "banana", subnet, ip)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.BridgeName).To(Equal("w1192-168-12-0"))
	})

	Context("when the subnet IP is of the form xxx.xxx.xxx.xxx", func() {
		BeforeEach(func() {
			var err error

			ip, subnet, err = net.ParseCIDR("123.122.180.191/24")
			Expect(err).NotTo(HaveOccurred())
		})

		It("does not assign a bridge name that is longer than 15 chars", func() {
			config, err := creator.Create(logger, "banana", subnet, ip)
			Expect(err).NotTo(HaveOccurred())

			Expect(len(config.BridgeName)).To(BeNumerically("<=", 15))
		})
	})

	It("it assigns the interface names based on the ID from the ID generator", func() {
		idGenerator.GenerateReturns("cocacola")

		config, err := creator.Create(logger, "bananashmanana", subnet, ip)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.HostIntf).To(Equal("w1cocacola-0"))
		Expect(config.ContainerIntf).To(Equal("w1cocacola-1"))
		Expect(config.IPTablePrefix).To(Equal("0123456789abcdef"))
		Expect(config.IPTableInstance).To(Equal("cocacola"))
	})

	It("only generates 1 ID per invocation", func() {
		_, err := creator.Create(logger, "bananashmanana", subnet, ip)
		Expect(err).NotTo(HaveOccurred())

		Expect(idGenerator.GenerateCallCount()).To(Equal(1))
	})

	It("saves the external ip", func() {
		config, err := creator.Create(logger, "banana", subnet, ip)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.ExternalIP.String()).To(Equal("220.10.120.5"))
	})

	It("saves the subnet and ip", func() {
		config, err := creator.Create(logger, "banana", subnet, ip)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.ContainerIP.String()).To(Equal("192.168.12.20"))
		Expect(config.Subnet.String()).To(Equal("192.168.12.0/24"))
	})

	It("assigns the bridge IP as the first IP in the subnet", func() {
		config, err := creator.Create(logger, "banana", subnet, ip)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.BridgeIP.String()).To(Equal("192.168.12.1"))
	})

	It("hard-codes the MTU to 1500", func() {
		config, err := creator.Create(logger, "banana", subnet, ip)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.Mtu).To(Equal(1500))
	})
})
