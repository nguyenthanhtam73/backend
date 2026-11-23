package domain

import "testing"

func TestTreeRewardCoinsAre420(t *testing.T) {
	if got := TreeRewardCoins(); got != 420 {
		t.Fatalf("coins %d", got)
	}
	stages := TreeStages(TreeItemClipLeaf, "Kẹp tóc lá xanh")
	if len(stages) != 7 || stages[0].Coins != 0 || stages[6].ItemID != TreeItemClipLeaf {
		t.Fatalf("ladder %+v", stages)
	}
	if stages[3].ItemID != TreeItemSticker || stages[5].ItemID != TreeItemNameFrame {
		t.Fatalf("shared items %+v %+v", stages[3], stages[5])
	}
}

func TestTreeTypeSeedEnablesFourAndHidesRares(t *testing.T) {
	enabled := map[string]bool{}
	for _, row := range TreeTypeSeed() {
		enabled[row.Key] = row.Enabled
		if row.ImageKey == "" || row.ExclusiveItemKey == "" || row.NameVI == "" {
			t.Fatalf("incomplete %+v", row)
		}
	}
	for _, key := range []string{TreeTypeLeaf, "cherry", "aloe", "sunflower"} {
		if !enabled[key] {
			t.Fatalf("%s should be enabled", key)
		}
	}
	for _, key := range []string{"dandelion", "quynh", "sen_hong"} {
		on, ok := enabled[key]
		if !ok || on {
			t.Fatalf("%s enabled=%v present=%v", key, on, ok)
		}
	}
	if TreeTypeCreatable("cherry") || !TreeTypeCreatable(TreeTypeLeaf) {
		t.Fatal("only leaf is plantable")
	}
	if RealRoomMember(RoomMemberAI) || !RealRoomMember("") || !RealRoomMember(RoomMemberUser) {
		t.Fatal("real member filter")
	}
}
