// +build windows

package hcsoci

// Contains functions relating to a LCOW container, as opposed to a utility VM

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/Microsoft/hcsshim/internal/schema2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

const rootfsPath = "rootfs"
const mountPathPrefix = "m"

func allocateLinuxResources(coi *createOptionsInternal, resources *Resources) error {
	if coi.Spec.Root == nil {
		coi.Spec.Root = &specs.Root{}
	}
	if coi.Spec.Root.Path == "" {
		logrus.Debugln("hcsshim::allocateLinuxResources mounting storage")
		mcl, err := MountContainerLayers(coi.Spec.Windows.LayerFolders, resources.containerRootInUVM, coi.HostingSystem)
		if err != nil {
			return fmt.Errorf("failed to mount container storage: %s", err)
		}
		if coi.HostingSystem == nil {
			coi.Spec.Root.Path = mcl.(string) // Argon v1 or v2
		} else {
			coi.Spec.Root.Path = mcl.(schema2.CombinedLayersV2).ContainerRootPath // v2 Xenon LCOW
		}
		resources.layers = coi.Spec.Windows.LayerFolders
	} else {
		// This is the "Plan 9" root filesystem.
		// TODO: We need a test for this. Ask @jstarks how you can even lay this out on Windows.
		hostPath := coi.Spec.Root.Path
		uvmPathForContainersFileSystem := path.Join(resources.containerRootInUVM, rootfsPath)
		var flags int32 = schema2.VPlan9FlagNone
		if coi.Spec.Root.Readonly {
			flags = schema2.VPlan9FlagReadOnly
		}
		err := coi.HostingSystem.AddPlan9(hostPath, uvmPathForContainersFileSystem, flags)
		if err != nil {
			return fmt.Errorf("adding plan9 root: %s", err)
		}
		coi.Spec.Root.Path = uvmPathForContainersFileSystem
		resources.plan9Mounts = append(resources.plan9Mounts, hostPath)
	}

	for i, mount := range coi.Spec.Mounts {
		if mount.Type != "bind" {
			continue
		}
		if mount.Destination == "" || mount.Source == "" {
			return fmt.Errorf("invalid OCI spec - a mount must have both source and a destination: %+v", mount)
		}

		if coi.HostingSystem != nil {
			logrus.Debugf("hcsshim::allocateLinuxResources Hot-adding Plan9 for OCI mount %+v", mount)

			hostPath := mount.Source
			uvmPathForShare := path.Join(resources.containerRootInUVM, mountPathPrefix+strconv.Itoa(i))

			var flags int32 = schema2.VPlan9FlagNone
			for _, o := range mount.Options {
				if strings.ToLower(o) == "ro" {
					flags = schema2.VPlan9FlagReadOnly
					break
				}
			}
			err := coi.HostingSystem.AddPlan9(hostPath, uvmPathForShare, flags)
			if err != nil {
				return fmt.Errorf("adding plan9 mount %+v: %s", mount, err)
			}
			coi.Spec.Mounts[i].Source = uvmPathForShare
			resources.plan9Mounts = append(resources.plan9Mounts, hostPath)
		}
	}

	return nil
}
