package adminstats

import "testing"

func TestClassifyRegistrationSource(t *testing.T) {
	sp := func(s string) *string { return &s }
	cases := []struct {
		name           string
		source, fb, tt *string
		want           string
	}{
		{name: "nil", want: sourceNone},
		{name: "blank source", source: sp("  "), want: sourceNone},
		{name: "blank click ids", fb: sp(" "), tt: sp("\t"), want: sourceNone},

		{name: "ttclid", tt: sp("tt.clid~1"), want: sourceTikTok},
		{name: "utm tiktok", source: sp("tiktok"), want: sourceTikTok},
		{name: "utm TikTok", source: sp("TikTok"), want: sourceTikTok},
		{name: "utm padded TIKTOK ads", source: sp("  TIKTOK_ads "), want: sourceTikTok},
		{name: "utm contains tiktok", source: sp("paid tiktok"), want: sourceTikTok},
		{name: "utm mytiktok", source: sp("mytiktok"), want: sourceTikTok},
		{name: "tik tok is not tiktok", source: sp("tik tok"), want: sourceOther},
		{name: "tt alone is other", source: sp("tt"), want: sourceOther},

		{name: "fbclid", fb: sp("IwAR123"), want: sourceFacebook},
		{name: "utm fb", source: sp("fb"), want: sourceFacebook},
		{name: "utm FB padded", source: sp(" FB "), want: sourceFacebook},
		{name: "utm facebook", source: sp("facebook"), want: sourceFacebook},
		{name: "utm Facebook", source: sp("Facebook"), want: sourceFacebook},
		{name: "utm ig", source: sp("ig"), want: sourceFacebook},
		{name: "utm IG", source: sp("IG"), want: sourceFacebook},
		{name: "utm instagram", source: sp("instagram"), want: sourceFacebook},
		{name: "utm Instagram padded", source: sp(" Instagram "), want: sourceFacebook},

		{name: "facebook ads is other", source: sp("facebook ads"), want: sourceOther},
		{name: "facebook.com is other", source: sp("facebook.com"), want: sourceOther},
		{name: "meta without fbclid is other", source: sp("meta"), want: sourceOther},
		{name: "insta is other", source: sp("insta"), want: sourceOther},
		{name: "google is other", source: sp("google"), want: sourceOther},

		{name: "meta plus fbclid is facebook", source: sp("meta"), fb: sp("IwAR123"), want: sourceFacebook},
		{name: "both click ids are tiktok", fb: sp("IwAR123"), tt: sp("tt.1"), want: sourceTikTok},
		{name: "ttclid beats utm facebook", source: sp("facebook"), tt: sp("tt.1"), want: sourceTikTok},
		{name: "utm tiktok beats fbclid", source: sp("tiktok"), fb: sp("IwAR123"), want: sourceTikTok},
		{name: "utm TikTok and ttclid", source: sp("TikTok"), tt: sp("tt.1"), want: sourceTikTok},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyRegistrationSource(tc.source, tc.fb, tc.tt)
			if got != tc.want {
				t.Fatalf("classify()=%s want %s", got, tc.want)
			}
		})
	}
}
