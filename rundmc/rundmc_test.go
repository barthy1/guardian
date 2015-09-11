package rundmc_test

import (
	"errors"

	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/guardian/gardener"
	"github.com/cloudfoundry-incubator/guardian/rundmc"
	"github.com/cloudfoundry-incubator/guardian/rundmc/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rundmc", func() {
	var (
		fakeDepot           *fakes.FakeDepot
		fakeContainerRunner *fakes.FakeContainerRunner

		containerizer *rundmc.Containerizer
	)

	BeforeEach(func() {
		fakeDepot = new(fakes.FakeDepot)
		fakeContainerRunner = new(fakes.FakeContainerRunner)
		containerizer = &rundmc.Containerizer{
			Depot:           fakeDepot,
			ContainerRunner: fakeContainerRunner,
		}

		fakeDepot.LookupStub = func(handle string) (string, error) {
			return "/path/to/" + handle, nil
		}
	})

	Describe("create", func() {
		It("should ask the depot to create a container", func() {
			containerizer.Create(gardener.DesiredContainerSpec{
				Handle: "exuberant!",
			})

			Expect(fakeDepot.CreateCallCount()).To(Equal(1))
			Expect(fakeDepot.CreateArgsForCall(0)).To(Equal("exuberant!"))
		})

		Context("when creating the depot directory fails", func() {
			It("returns an error", func() {
				fakeDepot.CreateReturns(errors.New("blam"))
				Expect(containerizer.Create(gardener.DesiredContainerSpec{
					Handle: "exuberant!",
				})).NotTo(Succeed())
			})
		})

		Context("when looking up the container fails", func() {
			It("returns an error", func() {
				fakeDepot.LookupReturns("", errors.New("blam"))
				Expect(containerizer.Create(gardener.DesiredContainerSpec{
					Handle: "exuberant!",
				})).NotTo(Succeed())
			})

			It("does not attempt to start the container", func() {
				fakeDepot.LookupReturns("", errors.New("blam"))
				containerizer.Create(gardener.DesiredContainerSpec{
					Handle: "exuberant!",
				})

				Expect(fakeContainerRunner.RunCallCount()).To(Equal(0))
			})
		})

		It("should start a container in the created directory", func() {
			containerizer.Create(gardener.DesiredContainerSpec{
				Handle: "exuberant!",
			})

			Expect(fakeContainerRunner.RunCallCount()).To(Equal(1))
			Expect(fakeContainerRunner.RunArgsForCall(0)).To(Equal("/path/to/exuberant!"))
		})
	})

	Describe("run", func() {
		It("should ask the execer to exec a process in the container", func() {
			containerizer.Run("some-handle", garden.ProcessSpec{Path: "hello"}, garden.ProcessIO{})
			Expect(fakeContainerRunner.ExecCallCount()).To(Equal(1))

			path, spec, _ := fakeContainerRunner.ExecArgsForCall(0)
			Expect(path).To(Equal("/path/to/some-handle"))
			Expect(spec.Path).To(Equal("hello"))
		})

		Context("when looking up the container fails", func() {
			It("returns an error", func() {
				fakeDepot.LookupReturns("", errors.New("blam"))
				_, err := containerizer.Run("some-handle", garden.ProcessSpec{}, garden.ProcessIO{})
				Expect(err).To(HaveOccurred())
			})

			It("does not attempt to exec the process", func() {
				fakeDepot.LookupReturns("", errors.New("blam"))
				containerizer.Run("some-handle", garden.ProcessSpec{}, garden.ProcessIO{})
				Expect(fakeContainerRunner.RunCallCount()).To(Equal(0))
			})
		})
	})
})
