package nesemu

type BlipReader struct {
	buf   []buf_t
	accum long
}

func (br *BlipReader) Begin(buf *BlipBuffer) int {
	br.buf = buf.buffer_
	br.accum = buf.reader_accum

	return buf.bass_shift
}
