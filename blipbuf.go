package nessndemu

import (
	"math"
)

type (
	buf_t_           = long
	blargg_err_t     = string
	blip_sample_t    = int16
	resampled_time_t = unsigned_long
)

const (
	widest_impulse_ = 24 // public
	accum_fract     = 15
	sample_offset   = 0x7F7F
)

type BlipBuffer struct {
	/* public */
	buffer_      []buf_t_
	buffer_size_ unsigned // FIXME(elemir): len(buffer_) should be enough

	factor_ unsigned_long
	offset_ resampled_time_t

	/* private */
	reader_accum    long
	bass_shift      int
	samples_per_sec long
	clocks_per_sec  long
	bass_freq_      int
	length_         int
}

func NewBlipBuffer() *BlipBuffer {
	return &BlipBuffer{
		samples_per_sec: 44100,
		factor_:         ^unsigned_long(0),
		bass_freq_:      16,
	}
}

const (
	ULONG_MAX = 0xFFFFFFFF
)

// SetSampleRate sets output sample rate and buffer length in milliseconds (1/1000 sec, defaults
// to 1/4 second), then clear buffer. Returns NULL on success, otherwise if there
// isn't enough memory, returns error without affecting current buffer setup.
func (b *BlipBuffer) SetSampleRate(new_rate long, msec int) {
	var new_size unsigned = (ULONG_MAX >> BLIP_BUFFER_ACCURACY) + 1 - widest_impulse_ - 64

	if msec != 0 {
		var s size_t = (int(new_rate)*(msec+1) + 999) / 1000
		if s < size_t(new_size) {
			new_size = unsigned(s)
		} else {
			require(false) // requested buffer length exceeds limit
		}
	}

	if b.buffer_size_ != new_size {
		b.buffer_ = make([]buf_t_, new_size+widest_impulse_)
	}

	b.buffer_size_ = new_size
	b.length_ = int(long(new_size)*1000/new_rate - 1)
	if msec != 0 {
		assert(b.length_ == msec) // ensure length is same as that passed in
	}

	b.samples_per_sec = new_rate
	if b.clocks_per_sec != 0 {
		b.SetClockRate(b.clocks_per_sec) // recalculate factor
	}

	b.SetBassFreq(b.bass_freq_) // recalculate shift
	b.Clear(true)
}

// SetClockRate sets number of source time units per second
func (b *BlipBuffer) SetClockRate(cps long) {
	b.clocks_per_sec = cps
	b.factor_ = (unsigned_long)(math.Floor((double(b.samples_per_sec)/double(cps)*(1<<BLIP_BUFFER_ACCURACY) + 0.5)))
}

// SetBassFreq sets frequency at which high-pass filter attenuation passes -3dB
func (b *BlipBuffer) SetBassFreq(freq int) {
	b.bass_freq_ = freq
	if freq == 0 {
		b.bass_shift = 31 // 32 or greater invokes undefined behavior elsewhere
		return
	}

	b.bass_shift = 1 + (int)(math.Floor(1.442695041*math.Log(0.124*float64(b.samples_per_sec)/float64(freq))))
	if b.bass_shift < 0 {
		b.bass_shift = 0
	}

	if b.bass_shift > 24 {
		b.bass_shift = 24

	}
}

// RemoveSamples removes 'count' samples from those waiting to be read
func (b *BlipBuffer) RemoveSamples(count long) {
	require(b.buffer_ != nil) // sample rate must have been set

	if count == 0 {
		return
	}

	b.RemoveSilence(count)

	// Allows synthesis slightly past time passed to end_frame(), as long as it's
	// not more than an output sample.
	// to do: kind of hacky, could add run_until() which keeps track of extra synthesis
	const copy_extra = 1

	// copy remaining samples to beginning and clear old samples
	var remain long = b.SamplesAvail() + widest_impulse_ + copy_extra

	for i := range remain {
		b.buffer_[i+count] = b.buffer_[i]
	}
	for i := range count {
		b.buffer_[remain+i] = sample_offset & 0xFF
	}
}

// SamplesAvail returns number of samples available for reading with ReadSamples
func (b *BlipBuffer) SamplesAvail() long {
	return (long)(b.offset_ >> BLIP_BUFFER_ACCURACY)
}

// Clear removes all available samples and clear buffer to silence. If 'entire_buffer' is
// false, just clear out any samples waiting rather than the entire buffer.
func (b *BlipBuffer) Clear(entire_buffer bool) {
	var count = b.SamplesAvail()
	if entire_buffer {
		count = long(b.buffer_size_)
	}
	b.offset_ = 0
	b.reader_accum = 0
	for i := range count + widest_impulse_ {
		b.buffer_[i] = sample_offset & 0xFF
	}
}

// ReadSamples reads at most 'max_samples' out of buffer into 'out', removing them from from
// the buffer. Return number of samples actually read and removed. If stereo is
// true, increment 'out' one extra time after writing each sample, to allow
// easy interleving of two channels into a stereo output buffer.
func (b *BlipBuffer) ReadSamples(out []blip_sample_t, max_samples long, stereo bool) long {
	require(b.buffer_ != nil) // sample rate must have been set

	var count long = b.SamplesAvail()
	if count > max_samples {
		count = max_samples
	}

	if count == 0 {
		return 0 // optimization
	}

	var sample_offset long = sample_offset
	var bass_shift int = b.bass_shift
	var accum long = b.reader_accum

	if !stereo {
		for i := range count {
			var s long = accum >> accum_fract
			accum -= accum >> bass_shift
			accum += (long(b.buffer_[i]) - sample_offset) << accum_fract
			out[i] = (blip_sample_t)(s)

			// clamp sample
			if long(int16(s)) != s {
				out[i-1] = blip_sample_t(0x7FFF - (s >> 24))
			}
		}
	} else {
		for i := range count {
			var s long = accum >> accum_fract

			accum -= accum >> bass_shift
			accum += (long(b.buffer_[i]) - sample_offset) << accum_fract
			out[i*2] = (blip_sample_t)(s)

			// clamp sample
			if long(int16(s)) != s {
				out[i*2-2] = blip_sample_t(0x7FFF - (s >> 24))
			}
		}
	}

	b.reader_accum = accum
	b.RemoveSamples(count)

	return count
}

func (b *BlipBuffer) RemoveSilence(count long) {
	assert(count <= b.SamplesAvail()) // tried to remove more samples than available
	b.offset_ -= resampled_time_t(count) << BLIP_BUFFER_ACCURACY
}

func (b *BlipBuffer) resampled_time(time blip_time_t) resampled_time_t {
	return resampled_time_t(time)*resampled_time_t(b.factor_) + b.offset_
}

func (b *BlipBuffer) resampled_duration(period int) resampled_time_t {
	return resampled_time_t(period) * resampled_time_t(b.factor_)
}

// EndFrame ends current time frame of specified duration and make its samples available
// (along with any still-unread samples) for reading with read_samples(). Begins
// a new time frame at the end of the current frame.
func (b *BlipBuffer) EndFrame(time blip_time_t) {
	b.offset_ += blip_resampled_time_t(time) * blip_resampled_time_t(b.factor_)
	assert(b.SamplesAvail() <= long(b.buffer_size_)) // time outside buffer length
}
