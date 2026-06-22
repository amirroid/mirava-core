package pkg

import "github.com/MiravaOrg/mirava-core/pkg/gradle"

type MavenMirrorService = gradle.MavenMirrorService

type MavenCheckSpeedParams = gradle.MavenCheckSpeedParams

type MavenCheckSpeedData = gradle.MavenCheckSpeedData

type MavenCheckPackageParams = gradle.MavenCheckPackageParams

type MavenCheckPackageData = gradle.MavenCheckPackageData

type MavenCheckStatusData = gradle.MavenCheckStatusData

func NewMavenMirrorService() *MavenMirrorService {
	return gradle.NewMavenMirrorService()
}
