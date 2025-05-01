package nessndemu

import (
	"fmt"

	"github.com/elemir/nessndemu/cbool"
)

type apu_register_values struct {
	// $4000 to $4013
	ages, regs [20]unsigned_char

	// Extra internal states.
	dpcm_bytes_left, dpcm_dac unsigned_char
}

type Osc struct {
	ages, regs  [4]unsigned_char
	reg_written [4]unsigned_char
	output      *BlipBuffer

	length_counter int // length counter (0 if unused by oscillator)
	delay          int // delay until next (potential) transition
	last_amp       int // last amplitude oscillator was outputting
	trigger        int
}

/*
struct Nes_Osc
{
	int period() const {
		return (regs [3] & 7) * 0x100 + (regs [2] & 0xff);
	}
	int update_amp( int amp ) {
		int delta = amp - last_amp;
		last_amp = amp;
		return delta;
	}
	virtual void set_output(Blip_Buffer* o)
	{
		output = o;
	}
};
*/

func (osc *Osc) clock_length(halt_mask int) {
	if osc.length_counter != 0 && int(osc.regs[0])&halt_mask == 0 {
		osc.length_counter--
	}
}

func (osc *Osc) update_amp(amp int) int {
	delta := amp - osc.last_amp
	osc.last_amp = amp

	return delta
}

func (osc *Osc) reset() {
	osc.delay = 0
	osc.last_amp = 0
	osc.ages = [4]byte{}
}

type Envelope struct {
	Osc

	envelope  int
	env_delay int
}

func (env *Envelope) clock_envelope() {
	var period int = int(env.regs[0] & 15)

	if env.reg_written[3] != 0 {
		env.reg_written[3] = 0
		env.env_delay = period
		env.envelope = 15
	} else if env.env_delay <= 0 {
		env.env_delay = period
		if env.envelope|int(env.regs[0]&0x20) != 0 {
			env.envelope = (env.envelope - 1) & 15
		}
	} else {
		env.env_delay--
	}
}

func (env *Envelope) volume() int {
	if env.length_counter == 0 {
		return 0
	}

	if env.regs[0]&0x10 == 0 {
		return env.envelope
	}

	return int(env.regs[0]) & 0x10
}

func (env *Envelope) reset() {
	env.envelope = 0
	env.env_delay = 0
	env.Osc.reset()
}

type Square struct {
	Envelope

	synth *BlipSynth
}

func (s *Square) clock_sweep(i int) {
}

func (s *Square) run(time cpu_time_t, param2 cpu_time_t) {
}

type Noise struct {
	Envelope

	synth    *BlipSynth
	noise    int
	pal_mode cbool.Bool
}

var noise_period_table = [2][16]cpu_time_t{{
	0x004, 0x008, 0x010, 0x020, 0x040, 0x060, 0x080, 0x0A0, // NTSC
	0x0CA, 0x0FE, 0x17C, 0x1FC, 0x2FA, 0x3F8, 0x7F2, 0xFE4,
}, {

	0x004, 0x008, 0x00E, 0x01E, 0x03C, 0x058, 0x076, 0x094, // PAL
	0x0BC, 0x0EC, 0x162, 0x1D8, 0x2C4, 0x3B0, 0x762, 0xEC2,
}}

func (n *Noise) run(time cpu_time_t, end_time cpu_time_t) {
	if n.output == nil {
		return
	}

	var volume int = n.volume()
	var amp int

	if n.noise&1 != 0 {
		amp = volume
	}

	var delta int = n.update_amp(amp)
	if delta != 0 {
		n.synth.offset(time, delta, n.output)
	}

	time += cpu_time_t(n.delay)
	if time < end_time {
		var mode_flag int = 0x80
		var tap int = 1
		if cbool.FromInt(int(n.regs[2]) & mode_flag) {
			tap = 6
		}

		var period int = int(noise_period_table[cbool.ToInt[int](n.pal_mode)][n.regs[2]&15])
		if !cbool.FromInt(volume) {
			for {
				feedback := (n.noise & 0x01) ^ ((n.noise >> tap) & 0x01)
				n.noise = (n.noise >> 1) | (feedback << 14)
				time += cpu_time_t(period)
				if time >= end_time {
					break
				}
			}
		} else {
			// using resampled time avoids conversion in synth.offset()
			var rperiod blip_resampled_time_t = n.output.resampled_duration(period)
			var rtime blip_resampled_time_t = n.output.resampled_time(time)

			fmt.Printf("time=%d rperiod=%d rtime=%d\n", time, rperiod, rtime)

			for {
				var feedback int = (n.noise & 0x01) ^ ((n.noise >> tap) & 0x01)
				n.noise = (n.noise >> 1) | (feedback << 14)

				amp = 0
				if n.noise&1 == 0 {
					amp = volume
				}
				delta = n.update_amp(amp)
				if delta != 0 {
					n.synth.offset_resampled(rtime, delta, n.output)
				}

				time += cpu_time_t(period)
				rtime += rperiod

				if time < end_time {
					break
				}
			}
		}
	}

	n.delay = int(time - end_time)
}

func (n *Noise) reset() {
	n.noise = 4141
	n.Envelope.reset()
}

type Triangle struct {
	Osc

	synth *BlipSynth
}

func (t *Triangle) clock_linear_counter() {
}

func (t *Triangle) run(time cpu_time_t, param2 cpu_time_t) {
}

const (
	loop_flag = 0x40
)

type DMC struct {
	Osc

	address     int // address of next byte to read
	period      int
	buf         int
	bits_remain int
	bits        int
	buf_empty   bool
	silence     bool

	dac        int
	paused_dac int

	nextIRQ     cpu_time_t
	irq_enabled cbool.Bool
	irq_flag    cbool.Bool
	pal_mode    cbool.Bool
	nonlinear   cbool.Bool

	rom_reader      func(any, cpu_addr_t) int
	rom_reader_data any

	apu   *APU
	synth *BlipSynth
}

func NewDMC() *DMC {
	dmc := DMC{
		bits_remain: 1,
		nextIRQ:     NESAPINoIRQ,
		period:      0x036,
		buf_empty:   true,
		silence:     true,
	}

	dmc.reset()

	return &dmc
}

func (d *DMC) run(time cpu_time_t, end_time cpu_time_t) {
	if d.output == nil {
		return

	}

	var delta int = d.update_amp(d.dac)
	if delta != 0 {
		d.synth.offset(time, delta, d.output)
	}

	time += cpu_time_t(d.delay)
	if time < end_time {
		var bits_remain int = d.bits_remain
		if d.silence && d.buf_empty {
			var count int = (int(end_time-time) + d.period - 1) / d.period
			bits_remain = (bits_remain-1+8-(count%8))%8 + 1
			time += cpu_time_t(count * d.period)
		} else {
			// Blip_Buffer* const output = this->output;
			var period int = d.period
			var bits int = d.bits
			var dac int = d.dac

			for {
				if !d.silence {
					var step int = (bits&1)*4 - 2
					bits >>= 1
					if unsigned(dac+step) <= 0x7F {
						dac += step
						d.synth.offset_inline(time, step, d.output)
					}
				}

				time += cpu_time_t(period)

				bits_remain--
				if bits_remain == 0 {
					bits_remain = 8
					if d.buf_empty {
						d.silence = true
					} else {
						d.silence = false
						bits = d.buf
						d.buf_empty = true
						d.fill_buffer()
					}
				}

				if time >= end_time {
					break
				}
			}

			d.dac = dac
			d.last_amp = dac
			d.bits = bits
		}
		d.bits_remain = bits_remain
	}

	d.delay = int(time - end_time)
}

func (d *DMC) start() {
	fmt.Printf("dmc started\n")
}

func (d *DMC) fill_buffer() {
	if d.buf_empty && d.length_counter != 0 {
		require(d.rom_reader != nil) // rom_reader must be set
		d.buf = d.rom_reader(d.rom_reader_data, cpu_addr_t(0x8000+d.address))
		d.address = (d.address + 1) & 0x7FFF
		d.buf_empty = false
		d.length_counter--
		if d.length_counter == 0 {
			if cbool.FromInt(d.regs[0] & loop_flag) {
				d.reload_sample()
			} else {
				d.apu.osc_enables &= ^0x10
				d.irq_flag = d.irq_enabled
				d.nextIRQ = NESAPINoIRQ
				d.apu.irq_changed()
			}
		}
	}
}

func (d *DMC) reload_sample() {
	d.address = 0x4000 + int(d.regs[2])*0x40
	d.length_counter = int(d.regs[3])*0x10 + 1
}

var (
	dmc_period_table = [2][16]short{{
		0x1ac, 0x17c, 0x154, 0x140, 0x11e, 0x0fe, 0x0e2, 0x0d6, // NTSC
		0x0be, 0x0a0, 0x08e, 0x080, 0x06a, 0x054, 0x048, 0x036,
	}, {
		0x18e, 0x161, 0x13c, 0x129, 0x10a, 0x0ec, 0x0d2, 0x0c7, // PAL (totally untested)
		0x0b1, 0x095, 0x084, 0x077, 0x062, 0x04e, 0x043, 0x032, // to do: verify PAL periods
	}}

	dac_table = [128]unsigned_char{
		0, 0, 1, 2, 2, 3, 3, 4, 5, 5, 6, 7, 7, 8, 8, 9,
		10, 10, 11, 11, 12, 13, 13, 14, 14, 15, 15, 16, 17, 17, 18, 18,
		19, 19, 20, 20, 21, 21, 22, 22, 23, 23, 24, 24, 25, 25, 26, 26,
		27, 27, 28, 28, 29, 29, 30, 30, 31, 31, 32, 32, 32, 33, 33, 34,
		34, 35, 35, 35, 36, 36, 37, 37, 38, 38, 38, 39, 39, 40, 40, 40,
		41, 41, 42, 42, 42, 43, 43, 44, 44, 44, 45, 45, 45, 46, 46, 47,
		47, 47, 48, 48, 48, 49, 49, 49, 50, 50, 50, 51, 51, 51, 52, 52,
		52, 53, 53, 53, 54, 54, 54, 55, 55, 55, 56, 56, 56, 57, 57, 57,
	}
)

func (d *DMC) write_register(addr int, data int) {
	if addr == 0 {
		d.period = int(dmc_period_table[cbool.ToInt[int](d.pal_mode)][data&15])
		d.irq_enabled = (data & 0xc0) == 0x80 // enabled only if loop disabled
		d.irq_flag = d.irq_flag && d.irq_enabled
		d.recalc_irq()
	} else if addr == 1 {
		if !d.nonlinear {
			// adjust last_amp so that "pop" amplitude will be properly non-linear
			// with respect to change in dac
			var old_amp int = int(dac_table[d.dac])
			d.dac = data & 0x7F
			var diff int = int(dac_table[d.dac]) - old_amp
			d.last_amp = d.dac - diff
		}

		d.dac = data & 0x7F
	}
}

func (d *DMC) recalc_irq() {
	var irq cpu_time_t = NESAPINoIRQ
	if d.irq_enabled && cbool.FromInt(d.length_counter) {
		irq = cpu_time_t(int(d.apu.lastTime) + d.delay +
			((d.length_counter-1)*8+d.bits_remain-1)*d.period + 1)
	}
	if irq != d.nextIRQ {
		d.nextIRQ = irq
		d.apu.irq_changed()
	}
}
