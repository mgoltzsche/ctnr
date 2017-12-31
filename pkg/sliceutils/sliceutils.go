package sliceutils

func AddToSet(set *[]string, entry string) bool {
	if *set == nil {
		*set = []string{entry}
		return true
	} else if !Contains(*set, entry) {
		*set = append(*set, entry)
		return true
	}
	return false
}

func RemoveFromSet(set *[]string, entry string) (removed bool) {
	if len(*set) > 0 {
		r := make([]string, 0, len(*set))
		for _, e := range *set {
			if e == entry {
				removed = true
			} else {
				r = append(r, e)
			}
		}
		*set = r
	}
	return
}

func Contains(set []string, entry string) (found bool) {
	if len(set) > 0 {
		for _, e := range set {
			if e == entry {
				found = true
				break
			}
		}
	}
	return
}
