package nesemu

import (
	"fmt"
	"math"

	"github.com/elemir/nessndemu/cbool"
)

const (
	BLIP_BUFFER_ACCURACY = 32
	BLIP_PHASE_BITS      = 6

	blip_res             = 1 << BLIP_PHASE_BITS
	blip_widest_impulse_ = 16
	blip_sample_bits     = 30

	blip_good_quality = 12
	blip_med_quality  = 8
)

type (
	imp_t = short

	blip_resampled_time_t = long_long
	blip_time_t           = long
)

type BlipSynth struct {
	quality int
	rng     int

	impulses []imp_t
	impl     *BlipSynth_
}

func NewBlipSynth(quality, rng int) *BlipSynth {
	bs := BlipSynth{
		quality: quality,
		rng:     rng,

		impulses: make([]imp_t, blip_res*(quality/2)+1),
	}

	bs.impl = NewBlipSynth_(bs.impulses, quality)

	return &bs
}

func (bs *BlipSynth) offset_resampled(time blip_resampled_time_t, delta int, blip_buf *BlipBuffer) {
	// Fails if time is beyond end of Blip_Buffer, due to a bug in caller code or the
	// need for a longer buffer as set by set_sample_rate().
	fmt.Printf("offset_resampled time %d %d\n", time, blip_buf.buffer_size_)
	assert((long)(time>>BLIP_BUFFER_ACCURACY) < blip_buf.buffer_size_)
	delta *= bs.impl.delta_factor
	var phase int = int(time >> (BLIP_BUFFER_ACCURACY - BLIP_PHASE_BITS) & (blip_res - 1))
	imp := bs.impulses[blip_res-phase:]
	buf := blip_buf.buffer_[time>>BLIP_BUFFER_ACCURACY:]
	var i0 long = long(imp[0])

	fwd := (blip_widest_impulse_ - bs.quality) / 2
	rev := fwd + bs.quality - 2

	forward := func(i int) {
		var t0 long = long(int(i0)*delta + int(buf[fwd+i]))
		var t1 long = long(int(imp[blip_res*(i+1)])*delta + int(buf[fwd+1+i]))

		i0 = long(imp[blip_res*(i+2)])
		buf[fwd+i] = t0
		buf[fwd+1+i] = t1
	}

	forward(0)
	if bs.quality > 8 {
		forward(2)
	}

	if bs.quality > 12 {
		forward(4)
	}

	{
		var mid int = bs.quality/2 - 1
		var t0 long = long(int(i0)*delta + int(buf[fwd+mid-1]))
		var t1 long = long(int(imp[blip_res*mid])*delta + int(buf[fwd+mid]))

		imp = bs.impulses[phase:]
		i0 = long(imp[blip_res*mid])
		buf[fwd+mid-1] = t0
		buf[fwd+mid] = t1
	}

	reverse := func(r int) {
		var t0 long = long(int(i0)*delta + int(buf[rev-r]))
		var t1 long = long(int(imp[blip_res*r])*delta + int(buf[rev+1-r]))

		i0 = long(imp[blip_res*(r-1)])
		buf[rev-r] = t0
		buf[rev+1-r] = t1
	}

	if bs.quality > 12 {
		reverse(6)
	}
	if bs.quality > 8 {
		reverse(4)
	}

	reverse(2)

	var t0 long = long(int(i0)*delta + int(buf[rev]))
	var t1 long = long(int(imp[1])*delta + int(buf[rev+1]))

	buf[rev] = t0
	buf[rev+1] = t1
}

func (b *BlipSynth) Volume(v float64) {
	rng := b.rng

	if rng < 0 {
		rng = -rng
	}

	b.impl.volume_unit(v * (1.0 / float64(rng)))
}

func (b *BlipSynth) offset(t blip_time_t, delta int, buf *BlipBuffer) {
	fmt.Printf("offset time=%d delta=%d factor=%d offset=%d\n", t, delta, buf.factor_, buf.offset_)
	b.offset_resampled(blip_resampled_time_t(t)*blip_resampled_time_t(buf.factor_)+buf.offset_, delta, buf)
}

/*
		long kernel_unit;
	public:
		Blip_Buffer* buf;
		int last_amp;
*/

type BlipSynth_ struct {
	volume_unit_ float64
	impulses     []short
	width        int
	kernel_unit  long

	delta_factor int
}

func NewBlipSynth_(p []short, w int) *BlipSynth_ {
	return &BlipSynth_{
		impulses: p,
		width:    w,
	}
}

func (bs *BlipSynth_) volume_unit(new_unit float64) {
	if new_unit != bs.volume_unit_ {
		// use default eq if it hasn't been set yet
		if cbool.FromInt(bs.kernel_unit) {
			bs.treble_eq(new_blip_eq_t(-8.0))
		}

		bs.volume_unit_ = new_unit
		var factor float64 = new_unit * (1 << blip_sample_bits) / float64(bs.kernel_unit)

		if factor > 0.0 {
			var shift int

			// if unit is really small, might need to attenuate kernel
			for factor < 2.0 {
				shift++
				factor *= 2.0
			}

			if shift > 0 {
				bs.kernel_unit >>= shift
				assert(bs.kernel_unit > 0) // fails if volume unit is too low

				// keep values positive to avoid round-towards-zero of sign-preserving
				// right shift for negative values
				var offset long = 0x8000 + (1 << (shift - 1))
				var offset2 long = 0x8000 >> shift
				for i := bs.impulses_size() - 1; i >= 0; i-- {
					bs.impulses[i] = short(((long(bs.impulses[i]) + offset) >> shift) - offset2)
				}
				bs.adjust_impulse()
			}
		}
		bs.delta_factor = int(math.Floor(factor + 0.5))
	}
}

func (bs *BlipSynth_) adjust_impulse() {
	// sum pairs for each phase and add error correction to end of first half
	var size int = bs.impulses_size()
	for p := blip_res; p >= blip_res/2; p-- {
		var p2 int = blip_res - 2 - p
		var error long = bs.kernel_unit

		for i := 1; i < size; i += blip_res {
			error -= long(bs.impulses[i+p])
			error -= long(bs.impulses[i+p2])
		}
		if p == p2 {
			error /= 2 // phase = 0.5 impulse uses same half for both sides
		}
		bs.impulses[size-blip_res+p] += short(error)
	}
}

func (bs *BlipSynth_) treble_eq(eq blip_eq_t) {
	fimpulse := make([]float32, blip_res/2*(blip_widest_impulse_-1)+blip_res*2)

	var half_size int = blip_res / 2 * (bs.width - 1)
	eq.generate(fimpulse[blip_res:], half_size)

	// need mirror slightly past center for calculation
	for i := blip_res; i > 0; i-- {
		fimpulse[blip_res+half_size+i] = fimpulse[blip_res+half_size-1-i]
	}

	// starts at 0
	for i := range blip_res {
		fimpulse[i] = 0.0
	}

	// find rescale factor
	var total float64
	for i := range half_size {
		total += float64(fimpulse[blip_res+i])

	}

	//double const base_unit = 44800.0 - 128 * 18; // allows treble up to +0 dB
	//double const base_unit = 37888.0; // allows treble to +5 dB
	var base_unit float64 = 32768.0 // necessary for blip_unscaled to work
	var rescale float64 = base_unit / 2 / total
	bs.kernel_unit = long(base_unit)

	// integrate, first difference, rescale, convert to int
	var sum, next float64
	var impulses_size int = bs.impulses_size()

	for i := range impulses_size {
		bs.impulses[i] = short(math.Floor((next-sum)*rescale + 0.5))
		sum += float64(fimpulse[i])
		next += float64(fimpulse[i+blip_res])
	}

	bs.adjust_impulse()

	// volume might require rescaling
	var vol float64 = bs.volume_unit_
	if vol != 0 {
		bs.volume_unit_ = 0.0
		bs.volume_unit(vol)
	}
}

func (bs *BlipSynth_) impulses_size() int {
	return blip_res/2*bs.width + 1
}

const (
	pi = 3.1415926535897932384626433832795029
)

type blip_eq_t struct {
	treble float64

	rolloff_freq long
	sample_rate  long
	cutoff_freq  long
}

/*
class blip_eq_t {
public:
	// See notes.txt
	blip_eq_t( double treble, long rolloff_freq, long sample_rate, long cutoff_freq = 0 );
};
*/

func new_blip_eq_t(treble_db float64) blip_eq_t {
	return blip_eq_t{
		treble:      treble_db,
		sample_rate: 44100,
	}
}

func (eq blip_eq_t) generate(out []float32, count int) {
	// lower cutoff freq for narrow kernels with their wider transition band
	// (8 points->1.49, 16 points->1.15)
	var oversample float64 = blip_res*2.25/float64(count) + 0.85
	var half_rate float64 = float64(eq.sample_rate) * 0.5
	if eq.cutoff_freq != 0 {
		oversample = half_rate / float64(eq.cutoff_freq)
	}

	var cutoff float64 = float64(eq.rolloff_freq) * oversample / half_rate

	gen_sinc(out, count, blip_res*oversample, eq.treble, cutoff)

	// apply (half of) hamming window
	var to_fraction float64 = pi / float64(count-1)
	for i := count - 1; i >= 0; i-- {
		out[i] *= float32(0.54 - 0.46*math.Cos(float64(i)*to_fraction))
	}
}

func gen_sinc(out []float32, count int, oversample, treble, cutoff float64) {
	if cutoff >= 0.999 {
		cutoff = 0.999

	}

	if treble < -300.0 {
		treble = -300.0
	}

	if treble > 5.0 {
		treble = 5.0

	}

	var maxh float64 = 4096.0
	var rolloff float64 = math.Pow(10.0, 1.0/(maxh*20.0)*treble/(1.0-cutoff))
	var pow_a_n float64 = math.Pow(rolloff, maxh-maxh*cutoff)
	var to_angle float64 = pi / 2 / maxh / oversample

	for i := range count {
		var angle float64 = float64((i-count)*2+1) * to_angle
		var c float64 = rolloff*math.Cos((maxh-1.0)*angle) - math.Cos(maxh*angle)
		var cos_nc_angle float64 = math.Cos(maxh * cutoff * angle)
		var cos_nc1_angle float64 = math.Cos((maxh*cutoff - 1.0) * angle)
		var cos_angle float64 = math.Cos(angle)

		c = c*pow_a_n - rolloff*cos_nc1_angle + cos_nc_angle
		var d float64 = 1.0 + rolloff*(rolloff-cos_angle-cos_angle)
		var b float64 = 2.0 - cos_angle - cos_angle
		var a float64 = 1.0 - cos_angle - cos_nc_angle + cos_nc1_angle

		out[i] = float32((a*d + c*b) / (b * d)) // a / b + c / d
	}
}
