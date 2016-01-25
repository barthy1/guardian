package rundmc

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudfoundry-incubator/garden"
	"github.com/cloudfoundry-incubator/goci"
	"github.com/cloudfoundry-incubator/goci/specs"
	"github.com/cloudfoundry-incubator/guardian/gardener"
)

//go:generate counterfeiter . MkdirChowner

type BaseTemplateRule struct {
	PrivilegedBase   *goci.Bndl
	UnprivilegedBase *goci.Bndl
}

func (r BaseTemplateRule) Apply(bndl *goci.Bndl, spec gardener.DesiredContainerSpec) *goci.Bndl {
	if spec.Privileged {
		return r.PrivilegedBase
	} else {
		return r.UnprivilegedBase
	}
}

type RootFSRule struct {
	ContainerRootUID int
	ContainerRootGID int

	MkdirChowner MkdirChowner
}

func (r RootFSRule) Apply(bndl *goci.Bndl, spec gardener.DesiredContainerSpec) *goci.Bndl {
	r.MkdirChowner.MkdirChown(filepath.Join(spec.RootFSPath, ".pivot_root"), 0700, r.ContainerRootUID, r.ContainerRootGID)
	return bndl.WithRootFS(spec.RootFSPath)
}

type NetworkHookRule struct {
	LogFilePattern string
}

func (r NetworkHookRule) Apply(bndl *goci.Bndl, spec gardener.DesiredContainerSpec) *goci.Bndl {
	return bndl.WithPrestartHooks(specs.Hook{
		Env: []string{fmt.Sprintf(
			"GARDEN_LOG_FILE="+r.LogFilePattern, spec.Handle),
			"PATH=" + os.Getenv("PATH"),
		},
		Path: spec.NetworkHook.Path,
		Args: spec.NetworkHook.Args,
	})
}

type MkdirChowner interface {
	MkdirChown(path string, perms os.FileMode, uid, gid int) error
}

type MkdirChownFunc func(path string, perms os.FileMode, uid, gid int) error

func (fn MkdirChownFunc) MkdirChown(path string, perms os.FileMode, uid, gid int) error {
	return fn(path, perms, uid, gid)
}

func MkdirChown(path string, perms os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(path, perms); err != nil {
		return err
	}

	return os.Chown(path, uid, gid)
}

type BindMountsRule struct {
}

func (b BindMountsRule) Apply(bndl *goci.Bndl, spec gardener.DesiredContainerSpec) *goci.Bndl {
	var mounts []goci.Mount
	for _, m := range spec.BindMounts {
		modeOpt := "ro"
		if m.Mode == garden.BindMountModeRW {
			modeOpt = "rw"
		}

		mounts = append(mounts, goci.Mount{
			Name:        m.DstPath,
			Destination: m.DstPath,
			Source:      m.SrcPath,
			Type:        "bind",
			Options:     []string{"bind", modeOpt},
		})
	}

	return bndl.WithMounts(mounts...)
}
