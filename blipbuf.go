package nesemu

import (
	"math"

	"github.com/elemir/nessndemu/cbool"
)

type (
	buf_t        = long
	blargg_err_t = string
)

const (
	buffer_extra = blip_widest_impulse_ + 2
)

type BlipBuffer struct {
	/* public */
	buffer_ []buf_t

	buffer_size_ long // FIXME(elemir): len(buffer_) should be enough

	factor_ unsigned_long
	offset_ blip_resampled_time_t

	/* private */
	reader_accum long
	bass_shift   int
	bass_freq_   int
	clock_rate_  long
	sample_rate_ long
	length_      int
}

func NewBlipBuffer() *BlipBuffer {
	return &BlipBuffer{
		factor_:    2147483647,
		bass_freq_: 16,
	}
}

// SetSampleRate sets output sample rate and buffer length in milliseconds (1/1000 sec, defaults
// to 1/4 second), then clear buffer. Returns NULL on success, otherwise if there
// isn't enough memory, returns error without affecting current buffer setup.
func (b *BlipBuffer) SetSampleRate(new_rate long, msec int) {
	if msec == 0 {
		msec = 1000 / 4
	}

	var new_size long = long((int(new_rate)*(msec+1) + 999) / 1000)

	if b.buffer_size_ != new_size {
		b.buffer_ = make([]buf_t, new_size+buffer_extra)
	}

	b.buffer_size_ = new_size

	// update things based on the sample rate
	b.sample_rate_ = new_rate
	b.length_ = int(new_size*1000/new_rate - 1)
	assert(b.length_ == msec) // ensure length is same as that passed in

	/*
	   	if b.clock_rate_ != 0 {
	   		b.clock_rate(b.clock_rate_)
	   	}

	   b.bass_freq(bass_freq_)

	   b.clear()
	*/
}

func (b *BlipBuffer) ClockRate(cps long) {
	b.clock_rate_ = cps
	b.factor_ = unsigned_long(b.clock_rate_factor(cps))
}

func (b *BlipBuffer) clock_rate_factor(clock_rate long) blip_resampled_time_t {
	var ratio float64 = float64(b.sample_rate_) / float64(clock_rate)
	var factor long = long(math.Floor(ratio*(1<<BLIP_BUFFER_ACCURACY) + 0.5))

	assert(bool(factor > 0 || !cbool.FromInt(b.sample_rate_))) // fails if clock/output ratio is too large

	return blip_resampled_time_t(factor)
}

func (b *BlipBuffer) resampled_time(time blip_time_t) blip_resampled_time_t {
	return blip_resampled_time_t(time)*blip_resampled_time_t(b.factor_) + b.offset_
}

func (b *BlipBuffer) resampled_duration(period int) blip_resampled_time_t {
	return blip_resampled_time_t(period) * blip_resampled_time_t(b.factor_)
}
