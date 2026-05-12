package service_test

import (
	"testing"

	"github.com/MiravaOrg/mirava-core/pkg/service"
)

func TestMainApt(t *testing.T) {
	mirava := service.CreateMiravaService()
	list := []string{"https://mirror.arvancloud.ir/ubuntu", "https://mirror.arvancloud.ir/debian"}

	for _, url := range list {
		ok, data, err := mirava.AptService.CheckSpeed(url, 10, true, nil)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("ok: %v | %+v	", ok, *data)
	}
}
