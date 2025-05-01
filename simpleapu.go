package nessndemu

type (
	Sample = blip_sample_t
)

type SimpleAPU struct {
	apu *APU
	buf *BlipBuffer

	frame_length blip_time_t
	time         blip_time_t
}

func NewSimpleAPU() *SimpleAPU {
	apu := NewAPU()
	buf := NewBlipBuffer()

	simpleAPU := &SimpleAPU{
		frame_length: 29780,
		apu:          apu,
		buf:          buf,
	}

	simpleAPU.DMCReader(nullDMCReader, nil)

	return simpleAPU
}

func nullDMCReader(any, cpu_addr_t) int {
	return 0x55 // causes dmc sample to be flat
}

func (s *SimpleAPU) DMCReader(callback func(any, cpu_addr_t) int, user_data any) {
	s.apu.DMCReader(callback, user_data)
}

// TODO(evgenii.omelchenko): use out len
func (s *SimpleAPU) ReadSamples(out []blip_sample_t, count long) long {
	return s.buf.ReadSamples(out, count, false)
}

// SampleRate sets output sample rate
func (s *SimpleAPU) SampleRate(sample_rate long) {
	s.apu.Output(s.buf)
	s.buf.SetClockRate(1789773)
	s.buf.SetSampleRate(sample_rate, 0)
}

// EndFrame ends a 1/60 sound frame
func (s *SimpleAPU) EndFrame() {
	s.time = 0
	s.frame_length ^= 1
	s.apu.EndFrame(s.frame_length)
	s.buf.EndFrame(s.frame_length)
}

// WriteRegister writes data to register (0x4000-0x4017, except 0x4014 and 0x4016)
func (s *SimpleAPU) WriteRegister(addr cpu_addr_t, data int) {
	s.apu.WriteRegister(s.clock(), addr, data)
}

func (s *SimpleAPU) clock() blip_time_t {
	s.time += 4

	return s.time
}
