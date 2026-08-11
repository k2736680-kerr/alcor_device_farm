// Package phoneprofile contains the controlled Phone-only Android SDK hardware
// profiles that the Device Farm is allowed to pass to the emulator container.
package phoneprofile

import "strings"

type Profile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Density int    `json:"density_dpi"`
}

// Catalog intentionally excludes tablet, TV, wear, automotive, desktop and XR
// profiles. IDs map to the pinned Android SDK's avdmanager profile names.
var Catalog = []Profile{
	{"small_phone", "Small Phone", 720, 1280, 320},
	{"medium_phone", "Medium Phone", 1080, 2400, 420},
	{"pixel_10_pro_xl", "Pixel 10 Pro XL", 1344, 2992, 480},
	{"pixel_10_pro_fold", "Pixel 10 Pro Fold", 2076, 2152, 390},
	{"pixel_10_pro", "Pixel 10 Pro", 1280, 2856, 480},
	{"pixel_10", "Pixel 10", 1080, 2424, 420},
	{"pixel_9a", "Pixel 9a", 1080, 2424, 420},
	{"pixel_9_pro_xl", "Pixel 9 Pro XL", 1344, 2992, 480},
	{"pixel_9_pro_fold", "Pixel 9 Pro Fold", 2076, 2152, 390},
	{"pixel_9_pro", "Pixel 9 Pro", 1280, 2856, 480},
	{"pixel_9", "Pixel 9", 1080, 2424, 420},
	{"pixel_8a", "Pixel 8a", 1080, 2400, 420},
	{"pixel_8_pro", "Pixel 8 Pro", 1344, 2992, 480},
	{"pixel_8", "Pixel 8", 1080, 2400, 420},
	{"pixel_fold", "Pixel Fold", 2208, 1840, 420},
	{"pixel_7a", "Pixel 7a", 1080, 2400, 420},
	{"pixel_7_pro", "Pixel 7 Pro", 1440, 3120, 560},
	{"pixel_7", "Pixel 7", 1080, 2400, 420},
	{"pixel_6a", "Pixel 6a", 1080, 2400, 420},
	{"pixel_6_pro", "Pixel 6 Pro", 1440, 3120, 560},
	{"pixel_6", "Pixel 6", 1080, 2400, 420},
}

func Find(id string) (Profile, bool) {
	for _, item := range Catalog {
		if item.ID == strings.TrimSpace(id) {
			return item, true
		}
	}
	return Profile{}, false
}
