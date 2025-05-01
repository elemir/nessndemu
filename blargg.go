package nesemu

func increment_saturate(x byte) byte {
	if int(x)+1 > 255 {
		return 255
	}

	return x + 1
}
