package protocol

import (
	"testing"

	protocoldao "github.com/RedDuck-Software/poolsea-go/dao/protocol"
	protocolsettings "github.com/RedDuck-Software/poolsea-go/settings/protocol"

	"github.com/RedDuck-Software/poolsea-go/tests/testutils/evm"
)

func TestRewardsSettings(t *testing.T) {

	// State snapshotting
	if err := evm.TakeSnapshot(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := evm.RevertSnapshot(); err != nil {
			t.Fatal(err)
		}
	})

	// Bootstrap a claimer & get claimer settings
	claimerPerc := 0.1
	if _, err := protocoldao.BootstrapClaimer(rp, "poolseaClaimNode", claimerPerc, ownerAccount.GetTransactor()); err != nil {
		t.Error(err)
	} else {
		if value, err := protocolsettings.GetRewardsClaimerPerc(rp, "poolseaClaimNode", nil); err != nil {
			t.Error(err)
		} else if value != claimerPerc {
			t.Errorf("Incorrect rewards claimer percent %f", value)
		}
		if value, err := protocolsettings.GetRewardsClaimerPercTimeUpdated(rp, "poolseaClaimNode", nil); err != nil {
			t.Error(err)
		} else if value == 0 {
			t.Errorf("Incorrect rewards claimer percent time updated %d", value)
		}
		if value, err := protocolsettings.GetRewardsClaimersPercTotal(rp, nil); err != nil {
			t.Error(err)
		} else if value == 0 {
			t.Errorf("Incorrect rewards claimers total percent %f", value)
		}
	}

	// Set & get rewards claim interval time
	var rewardsClaimIntervalTime uint64 = 1
	if _, err := protocolsettings.BootstrapRewardsClaimIntervalTime(rp, rewardsClaimIntervalTime, ownerAccount.GetTransactor()); err != nil {
		t.Error(err)
	} else if value, err := protocolsettings.GetRewardsClaimIntervalTime(rp, nil); err != nil {
		t.Error(err)
	} else if value != rewardsClaimIntervalTime {
		t.Error("Incorrect rewards claim interval time value")
	}

}
