package tagwidth

import "sort"

func contains(ranges []rune, point rune) bool {
	i := sort.Search(len(ranges)/2, func(i int) bool { return ranges[i*2+1] >= point })
	return i*2 < len(ranges) && ranges[i*2] <= point
}

// Units implements the fixed label-width policy; it does not measure font pixels.
func Units(name string) int {
	total, current := 0, 0
	emojiBase, joinable, joining, regional := false, false, false, false
	for _, point := range name {
		if point == 0x200d {
			joining = joinable
			continue
		}
		if (point == 0xfe0f || point == 0x20e3) && emojiBase && current > 0 {
			current = 2
			continue
		}
		if contains(marks[:], point) || point >= 0x1f3fb && point <= 0x1f3ff || point >= 0xe0020 && point <= 0xe007f {
			continue
		}
		width := 1
		if contains(wide[:], point) || contains(emojiPresentation[:], point) {
			width = 2
		}
		isRegional := point >= 0x1f1e6 && point <= 0x1f1ff
		if joining && contains(pictographic[:], point) || regional && isRegional {
			current = max(current, width)
			regional = false
		} else {
			total += current
			current = width
			regional = isRegional
		}
		emojiBase = contains(emoji[:], point)
		joinable = contains(pictographic[:], point)
		joining = false
	}
	return total + current
}
