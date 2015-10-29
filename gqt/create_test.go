package gqt_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/guardian/gqt/runner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("Creating a Container", func() {
	var client *runner.RunningGarden
	var container garden.Container

	FDescribe("Capabilities", func() {
		Context("when container is privileged", func() {
			BeforeEach(func() {
				client = startGarden()

				var err error
				container, err = client.Create(garden.ContainerSpec{Privileged: true})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have CAP_SYS_ADMIN capability", func() {
				pid := initProcessPID(container.Handle())
				stdout := gbytes.NewBuffer()
				status, err := gexec.Start(exec.Command("cat", fmt.Sprintf("/proc/%d/status", pid)), io.MultiWriter(stdout, GinkgoWriter), GinkgoWriter)

				Expect(err).NotTo(HaveOccurred())
				Eventually(status).Should(gexec.Exit(0))

				Expect(stdout).Should(gbytes.Say("CapInh:	00000000a80425fb"))
				Expect(stdout).Should(gbytes.Say("CapPrm:	00000000a80425fb"))
				Expect(stdout).Should(gbytes.Say("CapEff:	00000000a80425fb"))
				Expect(stdout).Should(gbytes.Say("CapBnd:	00000000a80425fb"))
			})
		})

		Context("when container is unprivileged", func() {
			BeforeEach(func() {
				client = startGarden()

				var err error
				container, err = client.Create(garden.ContainerSpec{Privileged: false})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have CAP_SYS_ADMIN capability", func() {
				pid := initProcessPID(container.Handle())
				stdout := gbytes.NewBuffer()
				status, err := gexec.Start(exec.Command("cat", fmt.Sprintf("/proc/%d/status", pid)), io.MultiWriter(stdout, GinkgoWriter), GinkgoWriter)

				Expect(err).NotTo(HaveOccurred())
				Eventually(status).Should(gexec.Exit(0))

				Expect(stdout).Should(gbytes.Say("CapInh:	0000000000000000"))
				Expect(stdout).Should(gbytes.Say("CapPrm:	0000003fffffffff"))
				Expect(stdout).Should(gbytes.Say("CapEff:	0000003fffffffff"))
				Expect(stdout).Should(gbytes.Say("CapBnd:	0000003fffffffff"))
			})
		})
	})

	Context("after creating a container without a specified handle", func() {
		BeforeEach(func() {
			client = startGarden()

			var err error
			container, err = client.Create(garden.ContainerSpec{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a depot subdirectory based on the container handle", func() {
			Expect(container.Handle()).NotTo(BeEmpty())
			Expect(filepath.Join(client.DepotDir, container.Handle())).To(BeADirectory())
			Expect(filepath.Join(client.DepotDir, container.Handle(), "config.json")).To(BeARegularFile())
		})

		It("should lookup the right container", func() {
			lookupContainer, lookupError := client.Lookup(container.Handle())

			Expect(lookupError).NotTo(HaveOccurred())
			Expect(lookupContainer).To(Equal(container))
		})

		DescribeTable("placing the container in to all namespaces", func(ns string) {
			pid := initProcessPID(container.Handle())
			hostNS, err := gexec.Start(exec.Command("ls", "-l", fmt.Sprintf("/proc/1/ns/%s", ns)), GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())

			containerNS, err := gexec.Start(exec.Command("ls", "-l", fmt.Sprintf("/proc/%d/ns/%s", pid, ns)), GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(containerNS).Should(gexec.Exit(0))
			Eventually(hostNS).Should(gexec.Exit(0))

			hostFD := strings.Split(string(hostNS.Out.Contents()), ">")[1]
			containerFD := strings.Split(string(containerNS.Out.Contents()), ">")[1]

			Expect(hostFD).NotTo(Equal(containerFD))

		},
			Entry("should place the container in to the NET namespace", "net"),
			Entry("should place the container in to the IPC namespace", "ipc"),
			Entry("should place the container in to the UTS namespace", "uts"),
			Entry("should place the container in to the PID namespace", "pid"),
			Entry("should place the container in to the MNT namespace", "mnt"),
		)

		Describe("destroying the container", func() {
			var process garden.Process

			BeforeEach(func() {
				var err error
				process, err = container.Run(garden.ProcessSpec{
					Path: "/bin/sh",
					Args: []string{
						"-c", "read x",
					},
				}, ginkgoIO)

				Expect(err).NotTo(HaveOccurred())
				Expect(client.Destroy(container.Handle())).To(Succeed())
			})

			It("should kill the containers init process", func() {
				pid := initProcessPID(container.Handle())
				var killExitCode = func() int {
					sess, err := gexec.Start(exec.Command("kill", "-0", fmt.Sprintf("%d", pid)), GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					sess.Wait(1 * time.Second)
					return sess.ExitCode()
				}

				Eventually(killExitCode, "5s").Should(Equal(1))
			})

			It("should destroy the container's depot directory", func() {
				Expect(filepath.Join(client.DepotDir, container.Handle())).NotTo(BeAnExistingFile())
			})
		})
	})

	Context("after creating a container with a specified handle", func() {
		BeforeEach(func() {
			client = startGarden()

			var mySpec garden.ContainerSpec
			mySpec = garden.ContainerSpec{
				Handle: "containerA",
			}

			var err error
			container, err = client.Create(mySpec)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should lookup the right container for the handle", func() {
			lookupContainer, lookupError := client.Lookup("containerA")

			Expect(lookupError).NotTo(HaveOccurred())
			Expect(lookupContainer).To(Equal(container))
		})
	})

})

func initProcessPID(handle string) int {
	Eventually(fmt.Sprintf("/run/opencontainer/containers/%s/state.json", handle)).Should(BeAnExistingFile())
	stateFile, err := os.Open(fmt.Sprintf("/run/opencontainer/containers/%s/state.json", handle))
	Expect(err).NotTo(HaveOccurred())

	state := struct {
		Pid int `json:"init_process_pid"`
	}{}

	Eventually(func() error {
		// state.json is sometimes empty immediately after creation, so keep
		// trying until it's valid json
		return json.NewDecoder(stateFile).Decode(&state)
	}).Should(Succeed())

	return state.Pid
}
