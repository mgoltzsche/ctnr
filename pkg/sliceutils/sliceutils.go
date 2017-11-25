package sliceutils

func AddToSet(set *[]string, add string) bool {
	if *set == nil {
		*set = []string{add}
		return true
	} else if !Contains(*set, add) {
		*set = append(*set, add)
		return true
	}
	return false
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
