package gardener_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/guardian/gardener"
	"github.com/cloudfoundry-incubator/guardian/gardener/fakes"
	"github.com/concourse/baggageclaim/volume"
	bcfakes "github.com/concourse/baggageclaim/volume/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"
)

var _ = Describe("Gardener", func() {
	var (
		networker        *fakes.FakeNetworker
		containerizer    *fakes.FakeContainerizer
		uidGenerator     *fakes.FakeUidGenerator
		strategyProvider *bcfakes.FakeStrategyProvider
		volumeRepo       *bcfakes.FakeRepository

		gdnr *gardener.Gardener
	)

	BeforeEach(func() {
		containerizer = new(fakes.FakeContainerizer)
		uidGenerator = new(fakes.FakeUidGenerator)
		networker = new(fakes.FakeNetworker)
		strategyProvider = new(bcfakes.FakeStrategyProvider)
		volumeRepo = new(bcfakes.FakeRepository)

		gdnr = &gardener.Gardener{
			Containerizer:    containerizer,
			UidGenerator:     uidGenerator,
			Networker:        networker,
			StrategyProvider: strategyProvider,
			VolumeRepository: volumeRepo,
			Logger:           lagertest.NewTestLogger("test"),
		}
	})

	Describe("creating a container", func() {
		Context("when a handle is specified", func() {
			It("asks the containerizer to create a container", func() {
				_, err := gdnr.Create(garden.ContainerSpec{Handle: "bob"})

				Expect(err).NotTo(HaveOccurred())
				Expect(containerizer.CreateCallCount()).To(Equal(1))
				_, spec := containerizer.CreateArgsForCall(0)
				Expect(spec.Handle).To(Equal("bob"))
			})

			It("passes the created network to the containerizer", func() {
				networker.NetworkStub = func(_ lager.Logger, handle, spec string) (string, error) {
					return "/path/to/netns/" + handle, nil
				}

				_, err := gdnr.Create(garden.ContainerSpec{
					Handle:  "bob",
					Network: "10.0.0.2/30",
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(containerizer.CreateCallCount()).To(Equal(1))
				_, spec := containerizer.CreateArgsForCall(0)
				Expect(spec.NetworkPath).To(Equal("/path/to/netns/bob"))
			})

			Context("when networker fails", func() {
				BeforeEach(func() {
					networker.NetworkReturns("", errors.New("booom!"))
				})

				It("returns an error", func() {
					_, err := gdnr.Create(garden.ContainerSpec{Handle: "bob"})
					Expect(err).To(MatchError("booom!"))
				})

				It("does not create a container", func() {
					gdnr.Create(garden.ContainerSpec{Handle: "bob"})
					Expect(containerizer.CreateCallCount()).To(Equal(0))
				})
			})

			It("passes the created volume path to the containerizer", func() {
				volumeRepo.CreateVolumeReturns(volume.Volume{Path: "/path/to/banana/rootfs"}, nil)

				_, err := gdnr.Create(garden.ContainerSpec{
					Handle: "bob",
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(volumeRepo.CreateVolumeCallCount()).To(Equal(1))

				Expect(containerizer.CreateCallCount()).To(Equal(1))
				_, spec := containerizer.CreateArgsForCall(0)
				Expect(spec.RootFSPath).To(Equal("/path/to/banana/rootfs"))
			})

			Context("when the VolumeRepository fails", func() {
				BeforeEach(func() {
					volumeRepo.CreateVolumeStub = func(_ volume.Strategy,
						_ volume.Properties, _ uint) (volume.Volume, error) {
						return volume.Volume{}, errors.New("Explode!")
					}
				})

				It("returns a sensible error", func() {
					_, err := gdnr.Create(garden.ContainerSpec{
						Handle: "bob",
					})
					Expect(err).To(MatchError(("Explode!")))
				})

				It("does not create a container", func() {
					gdnr.Create(garden.ContainerSpec{Handle: "bob"})
					Expect(containerizer.CreateCallCount()).To(Equal(0))
				})
			})

			It("correctly delegates to the strategyProvider", func() {
				strategy := volume.EmptyStrategy{}

				strategyProvider.ProvideStrategyReturns(strategy, nil)

				_, err := gdnr.Create(garden.ContainerSpec{
					Handle:     "bob",
					RootFSPath: "/orig/rootfs",
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(strategyProvider.ProvideStrategyCallCount()).To(Equal(1))
				Expect(strategyProvider.ProvideStrategyArgsForCall(0)).To(Equal("/orig/rootfs"))

				Expect(volumeRepo.CreateVolumeCallCount()).To(Equal(1))
				actualStrategy, _, _ := volumeRepo.CreateVolumeArgsForCall(0)
				Expect(actualStrategy).To(Equal(strategy))
			})

			Context("when the StrategyProvider fails", func() {
				BeforeEach(func() {
					strategyProvider.ProvideStrategyReturns(nil, errors.New("So many wombles!"))
				})

				It("returns a sensible error", func() {
					_, err := gdnr.Create(garden.ContainerSpec{
						Handle: "bob",
					})
					Expect(err).To(MatchError("So many wombles!"))
				})

				It("does not create a container", func() {
					gdnr.Create(garden.ContainerSpec{Handle: "bob"})
					Expect(containerizer.CreateCallCount()).To(Equal(0))
				})
			})

			It("returns the container that Lookup would return", func() {
				c, err := gdnr.Create(garden.ContainerSpec{Handle: "handle"})
				Expect(err).NotTo(HaveOccurred())

				d, err := gdnr.Lookup("handle")
				Expect(err).NotTo(HaveOccurred())
				Expect(c).To(Equal(d))
			})
		})

		Context("when no handle is specified", func() {
			It("assigns a handle to the container", func() {
				uidGenerator.GenerateReturns("generated-handle")

				_, err := gdnr.Create(garden.ContainerSpec{})

				Expect(err).NotTo(HaveOccurred())
				Expect(containerizer.CreateCallCount()).To(Equal(1))
				_, spec := containerizer.CreateArgsForCall(0)
				Expect(spec.Handle).To(Equal("generated-handle"))
			})

			It("returns the container that Lookup would return", func() {
				c, err := gdnr.Create(garden.ContainerSpec{})
				Expect(err).NotTo(HaveOccurred())

				d, err := gdnr.Lookup(c.Handle())
				Expect(err).NotTo(HaveOccurred())
				Expect(c).To(Equal(d))
			})
		})
	})

	Context("when having a container", func() {
		var container garden.Container

		BeforeEach(func() {
			var err error
			container, err = gdnr.Lookup("banana")
			Expect(err).NotTo(HaveOccurred())
		})

		Describe("running a process in a container", func() {
			It("asks the containerizer to run the process", func() {
				origSpec := garden.ProcessSpec{Path: "ripe"}
				origIO := garden.ProcessIO{
					Stdout: gbytes.NewBuffer(),
				}
				_, err := container.Run(origSpec, origIO)
				Expect(err).ToNot(HaveOccurred())

				Expect(containerizer.RunCallCount()).To(Equal(1))
				_, id, spec, io := containerizer.RunArgsForCall(0)
				Expect(id).To(Equal("banana"))
				Expect(spec).To(Equal(origSpec))
				Expect(io).To(Equal(origIO))
			})

			Context("when the containerizer fails to run a process", func() {
				BeforeEach(func() {
					containerizer.RunReturns(nil, errors.New("lost my banana"))
				})

				It("returns the error", func() {
					_, err := container.Run(garden.ProcessSpec{}, garden.ProcessIO{})
					Expect(err).To(MatchError("lost my banana"))
				})
			})
		})

		Describe("destroying a container", func() {
			It("asks the containerizer to destroy the container", func() {
				Expect(gdnr.Destroy(container.Handle())).To(Succeed())
				Expect(containerizer.DestroyCallCount()).To(Equal(1))
				_, handle := containerizer.DestroyArgsForCall(0)
				Expect(handle).To(Equal(container.Handle()))
			})
		})
	})
})
