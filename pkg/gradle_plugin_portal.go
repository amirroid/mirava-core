package pkg

import "github.com/MiravaOrg/mirava-core/pkg/gradle"

type GradlePluginPortalMirrorService = gradle.GradlePluginPortalMirrorService

type GradlePluginCheckSpeedParams = gradle.GradlePluginCheckSpeedParams

type GradlePluginCheckSpeedData = gradle.GradlePluginCheckSpeedData

type GradlePluginCheckPackageData = gradle.GradlePluginCheckPackageData

type GradlePluginCheckStatusData = gradle.GradlePluginCheckStatusData

func NewGradlePluginPortalMirrorService() *GradlePluginPortalMirrorService {
	return gradle.NewGradlePluginPortalMirrorService()
}
