package erc721

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/divergencetech/ethier/ethtest"
	"github.com/divergencetech/ethier/ethtest/openseatest"
	"github.com/divergencetech/ethier/ethtest/revert"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/h-fam/errdiff"
)

// Actors in the tests
const (
	deployer = iota
	tokenOwner
	approved
	vandal
	proxy

	numAccounts // last account + 1 ;)
)

// Token IDs
const (
	exists = iota
	notExists
)

func deploy(t *testing.T) (*ethtest.SimulatedBackend, *TestableERC721CommonEnumerable) {
	t.Helper()

	sim := ethtest.NewSimulatedBackendTB(t, numAccounts)
	_, _, nft, err := DeployTestableERC721CommonEnumerable(sim.Acc(deployer), sim)
	if err != nil {
		t.Fatalf("DeployTestableERC721CommonEnumerable() error %v", err)
	}

	if _, err := nft.Mint(sim.Acc(tokenOwner), big.NewInt(exists)); err != nil {
		t.Fatalf("Mint(%d) error %v", exists, err)
	}
	if _, err := nft.Approve(sim.Acc(tokenOwner), sim.Addr(approved), big.NewInt(exists)); err != nil {
		t.Fatalf("Approve(<approved account>, %d) error %v", exists, err)
	}

	return sim, nft
}

func TestModifiers(t *testing.T) {
	_, nft := deploy(t)

	tests := []struct {
		name           string
		tokenID        int64
		errDiffAgainst interface{}
	}{
		{
			name:           "existing token",
			tokenID:        exists,
			errDiffAgainst: nil,
		},
		{
			name:           "non-existent token",
			tokenID:        notExists,
			errDiffAgainst: "ERC721Common: Token doesn't exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := nft.MustExist(nil, big.NewInt(tt.tokenID))
			if diff := errdiff.Check(err, tt.errDiffAgainst); diff != "" {
				t.Errorf("MustExist([%s]), modified with tokenExists(); %s", tt.name, diff)
			}
		})
	}
}

func TestOnlyApprovedOrOwner(t *testing.T) {
	sim, nft := deploy(t)
	openseatest.DeployProxyRegistryTB(t, sim)

	tests := []struct {
		name           string
		account        *bind.TransactOpts
		errDiffAgainst interface{}
	}{
		{
			name:           "token owner",
			account:        sim.Acc(tokenOwner),
			errDiffAgainst: nil,
		},
		{
			name:           "approved",
			account:        sim.Acc(approved),
			errDiffAgainst: nil,
		},
		{
			name:           "vandal",
			account:        sim.Acc(vandal),
			errDiffAgainst: string(revert.ERC721ApproveOrOwner),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := nft.MustBeApprovedOrOwner(tt.account, big.NewInt(exists))
			if diff := errdiff.Check(err, tt.errDiffAgainst); diff != "" {
				t.Errorf("MustBeApprovedOrOwner([%s]), modified with onlyApprovedOrOwner(); %s", tt.name, diff)
			}
		})
	}
}

func TestOpenSeaProxyApproval(t *testing.T) {
	sim, nft := deploy(t)
	openseatest.DeployProxyRegistryTB(t, sim)

	tests := []struct {
		name string
		want bool
	}{
		{
			name: "before setting proxy",
			want: false,
		},
		// Note that the proxy is set between the tests
		{
			name: "after setting proxy",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nft.IsApprovedForAll(nil, sim.Addr(deployer), sim.Addr(proxy))
			if err != nil || got != tt.want {
				t.Errorf("IsApprovedForAll([owner], [proxy]) got %t, err = %v; want %t, nil err", got, err, tt.want)
			}
		})

		openseatest.SetProxyTB(t, sim, sim.Addr(deployer), sim.Addr(proxy))
	}
}

func TestEnumerableInterface(t *testing.T) {
	enumerableMethods := []string{"TotalSupply", "TokenOfOwnerByIndex", "TokenByIndex"}
	// Common methods, expected on both, are used as a control because there is
	// significant embedding by abigen so we need to know which type actually
	// has the methods.
	commonMethods := []string{"BalanceOf", "OwnerOf", "IsApprovedForAll"}

	tests := []struct {
		contract interface{}
		methods  []string
		want     bool
	}{
		{
			contract: &ERC721CommonCaller{},
			methods:  commonMethods,
			want:     true,
		},
		{
			contract: &ERC721CommonEnumerableCaller{},
			methods:  commonMethods,
			want:     true,
		},
		{
			contract: &ERC721CommonCaller{},
			methods:  enumerableMethods,
			want:     false,
		},
		{
			contract: &ERC721CommonEnumerableCaller{},
			methods:  enumerableMethods,
			want:     true,
		},
	}

	for _, tt := range tests {
		typ := reflect.TypeOf(tt.contract)
		for _, method := range tt.methods {
			if _, got := typ.MethodByName(method); got != tt.want {
				t.Errorf("%T has method %q? got %t; want %t", tt.contract, method, got, tt.want)
			}
		}
	}

	// Effectively the same as above but this won't compile if we haven't
	// inherited properly.
	var enum ERC721CommonEnumerable
	_ = []interface{}{
		enum.TotalSupply,
		enum.TokenOfOwnerByIndex,
		enum.TokenByIndex,
	}
}

func TestBaseTokenURI(t *testing.T) {
	sim, nft := deploy(t)

	for _, id := range []int64{1, 42, 101010} {
		sim.Must(t, "Mint(%d)", id)(nft.Mint(sim.Acc(deployer), big.NewInt(id)))
	}

	wantURI := func(t *testing.T, id int64, want string) {
		t.Helper()
		got, err := nft.TokenURI(nil, big.NewInt(id))
		if err != nil || got != want {
			t.Errorf("tokenURI(%d) got %q, err = %v; want %q, nil err", id, got, err, want)
		}
	}

	// OpenZeppelin's ERC721 returns an empty string if no base is set.
	wantURI(t, 1, "")
	if diff := revert.OnlyOwner.Diff(nft.SetBaseTokenURI(sim.Acc(vandal), "bad")); diff != "" {
		t.Errorf("SetBaseTokenURI([as vandal]) %s", diff)
	}
	wantURI(t, 1, "")
	wantURI(t, 42, "")

	const base = "good/"
	sim.Must(t, "SetBaseTokenURI(%q)", base)(nft.SetBaseTokenURI(sim.Acc(deployer), base))
	wantURI(t, 42, "good/42")
	wantURI(t, 101010, "good/101010")
}
