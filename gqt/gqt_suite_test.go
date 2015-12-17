package gqt_test

import (
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/guardian/gqt/runner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"encoding/json"
	"testing"
)

var OciRuntimeBin = os.Getenv("OCI_RUNTIME")

var defaultRuntime = map[string]string{
	"linux": "runc",
}

var ginkgoIO = garden.ProcessIO{Stdout: GinkgoWriter, Stderr: GinkgoWriter}

var gardenBin, iodaemonBin, nstarBin string

func TestGqt(t *testing.T) {
	RegisterFailHandler(Fail)

	SynchronizedBeforeSuite(func() []byte {
		var err error
		bins := make(map[string]string)

		bins["garden_bin_path"], err = gexec.Build("github.com/cloudfoundry-incubator/guardian/cmd/guardian", "-tags", "daemon")
		Expect(err).NotTo(HaveOccurred())

		bins["iodaemon_bin_path"], err = gexec.Build("github.com/cloudfoundry-incubator/guardian/rundmc/iodaemon/cmd/iodaemon")
		Expect(err).NotTo(HaveOccurred())

		cmd := exec.Command("make")
		cmd.Dir = "../rundmc/nstar"
		cmd.Stdout = GinkgoWriter
		cmd.Stderr = GinkgoWriter
		Expect(cmd.Run()).To(Succeed())
		bins["nstar_bin_path"] = "../rundmc/nstar/nstar"

		data, err := json.Marshal(bins)
		Expect(err).NotTo(HaveOccurred())

		return data
	}, func(data []byte) {
		bins := make(map[string]string)
		Expect(json.Unmarshal(data, &bins)).To(Succeed())

		gardenBin = bins["garden_bin_path"]
		iodaemonBin = bins["iodaemon_bin_path"]
		nstarBin = bins["nstar_bin_path"]
	})

	BeforeEach(func() {
		if OciRuntimeBin == "" {
			OciRuntimeBin = defaultRuntime[runtime.GOOS]
		}

		if OciRuntimeBin == "" {
			Skip("No OCI Runtime for Platform: " + runtime.GOOS)
		}
	})

	SetDefaultEventuallyTimeout(5 * time.Second)
	RunSpecs(t, "GQT Suite")
}

func startGarden(argv ...string) *runner.RunningGarden {
	return runner.Start(gardenBin, iodaemonBin, nstarBin, argv...)
}
