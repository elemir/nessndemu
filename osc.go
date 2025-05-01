package nesemu

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

	fmt.Printf("run time=%d delay=%d\n", time, n.delay)
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
				if time < end_time {
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

/*
	int address;    // address of next byte to read
	int period;
	//int length_counter; // bytes remaining to play (already defined in Nes_Osc)
	int buf;
	int bits_remain;
	int bits;
	bool buf_full;
	bool silence;

	enum { loop_flag = 0x40 };

	int dac;
	int paused_dac;

	cpu_time_t next_irq;
	bool irq_enabled;
	bool irq_flag;
	bool pal_mode;
	bool nonlinear;
*/

type DMC struct {
	Osc

	nextIRQ   cpu_time_t
	irq_flag  cbool.Bool
	nonlinear cbool.Bool

	rom_reader      func(*void, cpu_addr_t) int
	rom_reader_data *void

	apu   *APU
	synth *BlipSynth
}

func (d *DMC) SetOutput(bufferTnd *BlipBuffer) {
}

func (d *DMC) run(time cpu_time_t, param2 cpu_time_t) {
}

func (d *DMC) start() {
}

func (d *DMC) write_register(reg int, data int) {
}
