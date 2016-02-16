package gqt_test

import (
	"io"

	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/guardian/gqt/runner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/types"
)

var _ = Describe("Run", func() {
	var client *runner.RunningGarden

	AfterEach(func() {
		Expect(client.DestroyAndStop()).To(Succeed())
	})

	DescribeTable("running a process",
		func(spec garden.ProcessSpec, matchers ...func(actual interface{})) {
			client = startGarden()
			container, err := client.Create(garden.ContainerSpec{})
			Expect(err).NotTo(HaveOccurred())

			out := gbytes.NewBuffer()
			proc, err := container.Run(
				spec,
				garden.ProcessIO{
					Stdout: io.MultiWriter(GinkgoWriter, out),
					Stderr: io.MultiWriter(GinkgoWriter, out),
				})
			Expect(err).NotTo(HaveOccurred())

			exitCode, err := proc.Wait()
			Expect(err).NotTo(HaveOccurred())

			for _, m := range matchers {
				m(&process{exitCode, out})
			}
		},

		Entry("with an absolute path",
			spec("/bin/sh", "-c", "echo hello; exit 12"),
			should(gbytes.Say("hello"), gexec.Exit(12)),
		),

		Entry("with a path to be found in a regular user's path",
			spec("sh", "-c", "echo potato; exit 24"),
			should(gbytes.Say("potato"), gexec.Exit(24)),
		),

		Entry("with a path that doesn't exist",
			spec("potato"),
			shouldNot(gexec.Exit(0)),
		),
	)

	Context("when container is privileged", func() {
		It("can run a process as a particular user", func() {
			client = startGarden()
			container, err := client.Create(garden.ContainerSpec{
				Privileged: true,
			})
			Expect(err).NotTo(HaveOccurred())

			out := gbytes.NewBuffer()
			proc, err := container.Run(
				garden.ProcessSpec{
					Path: "whoami",
					User: "alice",
				},
				garden.ProcessIO{
					Stdout: io.MultiWriter(GinkgoWriter, out),
					Stderr: io.MultiWriter(GinkgoWriter, out),
				})
			Expect(err).NotTo(HaveOccurred())

			exitCode, err := proc.Wait()
			Expect(err).NotTo(HaveOccurred())
			Expect(exitCode).To(Equal(0))

			Expect(out).To(gbytes.Say("alice"))
		})
	})

	FDescribe("working directory", func() {
		var container garden.Container

		BeforeEach(func() {
			var err error

			client = startGarden()
			container, err = client.Create(garden.ContainerSpec{})
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when the directory exists", func() {
			It("should run a process using it as a current working directory", func() {
				out := gbytes.NewBuffer()
				proc, err := container.Run(
					garden.ProcessSpec{
						Path: "pwd",
						Dir:  "/home/alice",
						User: "alice",
					},
					garden.ProcessIO{
						Stdout: io.MultiWriter(GinkgoWriter, out),
						Stderr: io.MultiWriter(GinkgoWriter, out),
					})
				Expect(err).NotTo(HaveOccurred())

				exitCode, err := proc.Wait()
				Expect(err).NotTo(HaveOccurred())
				Expect(exitCode).To(Equal(0))

				Expect(out).To(gbytes.Say("/home/alice"))
			})

			Context("and the user doesn't have permissions to it", func() {
				It("should fail with 'permission denied'", func() {
					out := gbytes.NewBuffer()
					_, err := container.Run(
						garden.ProcessSpec{
							Path: "pwd",
							Dir:  "/root",
							User: "alice",
						},
						garden.ProcessIO{
							Stdout: io.MultiWriter(GinkgoWriter, out),
							Stderr: io.MultiWriter(GinkgoWriter, out),
						})
					Expect(err).To(MatchError("PermissionDenied"))

					// exitCode, err := proc.Wait()
					// Expect(err).NotTo(HaveOccurred())
					// Expect(exitCode).To(Equal(0))
				})
			})
		})

		Context("when the directory doesn't exist", func() {
			It("should create the dir and run a process in it", func() {
				out := gbytes.NewBuffer()
				proc, err := container.Run(
					garden.ProcessSpec{
						Path: "pwd",
						Dir:  "/home/alice/notexist",
						User: "alice",
					},
					garden.ProcessIO{
						Stdout: io.MultiWriter(GinkgoWriter, out),
						Stderr: io.MultiWriter(GinkgoWriter, out),
					})
				Expect(err).NotTo(HaveOccurred())

				exitCode, err := proc.Wait()
				Expect(err).NotTo(HaveOccurred())
				Expect(exitCode).To(Equal(0))

				Expect(out).To(gbytes.Say("/home/alice/notexist"))
			})

			It("should create the dir with the correct ownership", func() {
				out := gbytes.NewBuffer()
				proc, err := container.Run(
					garden.ProcessSpec{
						Path: "stat",
						Args: []string{"-f", "%u,%g", "."},
						Dir:  "/home/alice/notexist",
						User: "alice",
					},
					garden.ProcessIO{
						Stdout: io.MultiWriter(GinkgoWriter, out),
						Stderr: io.MultiWriter(GinkgoWriter, out),
					})
				Expect(err).NotTo(HaveOccurred())

				exitCode, err := proc.Wait()
				Expect(err).NotTo(HaveOccurred())
				Expect(exitCode).To(Equal(0))

				Expect(out).To(gbytes.Say("1001,1001"))
			})

			Context("and use doesn't have permissions to it", func() {
				It("should fail with 'permission denied'", func() {
					out := gbytes.NewBuffer()
					_, err := container.Run(
						garden.ProcessSpec{
							Path: "pwd",
							Dir:  "/root/a_dir",
							User: "alice",
						},
						garden.ProcessIO{
							Stdout: io.MultiWriter(GinkgoWriter, out),
							Stderr: io.MultiWriter(GinkgoWriter, out),
						})
					Expect(err).To(MatchError("PermissionDenied"))

					// exitCode, err := proc.Wait()
					// Expect(err).NotTo(HaveOccurred())
					// Expect(exitCode).To(Equal(0))
				})
			})
		})
	})

	Describe("PATH env variable", func() {
		var container garden.Container

		BeforeEach(func() {
			client = startGarden()
			var err error
			container, err = client.Create(garden.ContainerSpec{})
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("contains the correct default values", func(user, path string) {
			out := gbytes.NewBuffer()
			proc, err := container.Run(
				garden.ProcessSpec{
					Path: "sh",
					Args: []string{"-c", "echo $PATH"},
					User: user,
				},
				garden.ProcessIO{
					Stdout: io.MultiWriter(GinkgoWriter, out),
					Stderr: io.MultiWriter(GinkgoWriter, out),
				})
			Expect(err).NotTo(HaveOccurred())

			exitCode, err := proc.Wait()
			Expect(err).NotTo(HaveOccurred())
			Expect(exitCode).To(Equal(0))

			Expect(out).To(gbytes.Say(path))
		},
			Entry("for a non-root user", "alice", `^/usr/local/bin:/usr/bin:/bin\n$`),
			Entry("for the root user", "root",
				`^/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n$`),
		)
	})

	Describe("Signalling", func() {
		It("should forward SIGTERM to the process", func(done Done) {
			client = startGarden()

			container, err := client.Create(garden.ContainerSpec{})
			Expect(err).NotTo(HaveOccurred())

			buffer := gbytes.NewBuffer()
			proc, err := container.Run(garden.ProcessSpec{
				Path: "sh",
				Args: []string{"-c", `
					trap 'exit 42' TERM
					echo 'trapping'

					sleep 100 &
					wait
				`},
			}, garden.ProcessIO{
				Stdout: buffer,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(buffer).Should(gbytes.Say("trapping"))

			err = proc.Signal(garden.SignalTerminate)
			Expect(err).NotTo(HaveOccurred())

			Expect(proc.Wait()).To(Equal(42))

			close(done)
		}, 20.0)
	})
})

func should(matchers ...types.GomegaMatcher) func(actual interface{}) {
	return func(actual interface{}) {
		for _, matcher := range matchers {
			Expect(actual).To(matcher)
		}
	}
}

func shouldNot(matchers ...types.GomegaMatcher) func(actual interface{}) {
	return func(actual interface{}) {
		for _, matcher := range matchers {
			Expect(actual).ToNot(matcher)
		}
	}
}

func spec(path string, args ...string) garden.ProcessSpec {
	return garden.ProcessSpec{
		Path: path,
		Args: args,
	}
}

type process struct {
	exitCode int
	buffer   *gbytes.Buffer
}

func (p *process) ExitCode() int {
	return p.exitCode
}

func (p *process) Buffer() *gbytes.Buffer {
	return p.buffer
}
