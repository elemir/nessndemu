package nessndemu

// BlipReader is an optimized inline sample reader for custom sample formats
// and mixing of BlipBuffer samples
type BlipReader struct {
	buf   []buf_t_
	accum long
}

// Next advances to next sample
func (br *BlipReader) Next(bass_shift int) {
	br.accum += br.buf[0] - (br.accum >> bass_shift)
	br.buf = br.buf[1:]
}

// Read returns current sample
func (br *BlipReader) Read() long {
	return br.accum >> (blip_sample_bits - 16)
}

// Begin reading samples from buffer. Returns value to pass to next() (can
// be ignored if default bass_freq is acceptable).
func (br *BlipReader) Begin(buf *BlipBuffer) int {
	br.buf = buf.buffer_
	br.accum = buf.reader_accum

	return buf.bass_shift
}

// End reading samples from buffer. The number of samples read must now be removed
// using Blip_Buffer::remove_samples().
func (br *BlipReader) End(b *BlipBuffer) {
	b.reader_accum = br.accum
}
