/* Copyright (C) 2003-2005 Shay Green.

This module is free software; you can redistribute it and/or modify
it under the terms of the GNU Lesser General Public
License as published by the Free Software Foundation; either
version 2.1 of the License, or (at your option) any later version.

This module is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Lesser General Public License for more details.

You should have received a copy of the GNU Lesser General Public
License along with this module; if not, write to the Free Software
Foundation, Inc., 59 Temple Place, Suite 330, Boston, MA 02111-1307 USA

*/

package nesemu

import (
	"fmt"

	"github.com/elemir/nessndemu/cbool"
)

const (
	NESAPUStartAddr  = 0x4000
	NESAPUEndAddr    = 0x4017
	NESAPUStatusAddr = 0x4015

	NESAPUOscCount = 5

	NESAPINoIRQ      = 1073741824
	NESAPUIRQWaiting = 0

	NESAPUShadowRegsCount = 21
)

// TODO(FIXME): we should understand what types should be here
type (
	void       = any
	cpu_addr_t = unsigned
	cpu_time_t = long
)

type APU struct {
	oscs             [NESAPUOscCount]*Osc
	square1, square2 Square
	noise            Noise
	triangle         Triangle
	dmc              DMC
	square_synth     *BlipSynth

	lastTime    cpu_time_t // has been run until this time in current frame
	nextIRQ     cpu_time_t
	earliestIRQ cpu_time_t

	framePeriod int
	frameDelay  int // cycles until frame counter runs next
	frame       int // current frame (0-3)

	osc_enables int
	frameMode   int

	irqFlag cbool.Bool

	irqNotifier func(user_data *void)
	irqData     *void
}

func NewAPU() *APU {
	var apu APU

	apu.square_synth = NewBlipSynth(blip_good_quality, 30)

	apu.dmc.apu = &apu
	apu.square1.synth = apu.square_synth
	apu.square2.synth = apu.square_synth
	apu.triangle.synth = NewBlipSynth(blip_good_quality, 15)
	apu.noise.synth = NewBlipSynth(blip_good_quality, 15)
	apu.dmc.synth = NewBlipSynth(blip_med_quality, 127)

	apu.oscs[0] = &apu.square1.Osc
	apu.oscs[1] = &apu.square2.Osc
	apu.oscs[2] = &apu.triangle.Osc
	apu.oscs[3] = &apu.noise.Osc
	apu.oscs[4] = &apu.dmc.Osc

	apu.Volume(1.0)
	apu.Reset(false, 0)

	return &apu
}

func (apu *APU) Reset(pal_mode cbool.Bool, initial_dmc_dac int) {
	apu.framePeriod = 7458
	if pal_mode {
		apu.framePeriod = 8314
	}

	// apu.dmc.pal_mode = pal_mode
	apu.noise.pal_mode = pal_mode

	apu.square1.reset()
	apu.square2.reset()
	apu.triangle.reset()
	apu.noise.reset()
	apu.dmc.reset()

	apu.lastTime = 0
	apu.osc_enables = 0
	apu.irqFlag = false
	apu.earliestIRQ = NESAPINoIRQ
	apu.frameDelay = 1
	apu.WriteRegister(0, 0x4017, 0x00)
	apu.WriteRegister(0, 0x4015, 0x00)

	for addr := cpu_addr_t(NESAPUStartAddr); addr <= 0x4013; addr++ {
		var data int
		if !cbool.FromInt(addr & 3) {
			data = 0x10
		}

		apu.WriteRegister(0, addr, data)
	}

	/*
		apu.dmc.dac = initial_dmc_dac
		if !apu.dmc.nonlinear {
			apu.dmc.last_amp = initial_dmc_dac // prevent output transition
		}
	*/

	apu.ResetTriggers()
}

const (
	TriggerNone = -2 // Unable to provide trigger, must use fallback.
	TriggerHold = -1 // A valid trigger should be coming, hold previous valid one until.
)

func (apu *APU) ResetTriggers() {
	apu.square1.trigger = TriggerHold
	apu.square2.trigger = TriggerHold
	apu.triangle.trigger = TriggerHold
	apu.noise.trigger = TriggerNone // Not implemented yet, would be nice to support mode 1 (93/31 sequence)
	apu.dmc.trigger = TriggerNone   // Looping samples would be nice to support.
}

func (apu *APU) Output(buffer *BlipBuffer, bufferTnd *BlipBuffer) {
	for i := range 2 {
		apu.oscs[i].output = buffer
	}

	for i := range 2 {
		apu.oscs[i+2].output = bufferTnd
	}

	apu.dmc.SetOutput(bufferTnd)
}

func (apu *APU) Volume(v float64) {
	apu.dmc.nonlinear = false

	// Should be 0.00752 * 15, but i find this to be a better approximation.
	apu.square_synth.Volume(0.00861 * 15 * v)
	apu.triangle.synth.Volume(0.12765 * v)
	apu.noise.synth.Volume(0.095 * v)
	apu.dmc.synth.Volume(0.42545 * v)
}

func (apu *APU) DMCReader(reader func(*void, cpu_addr_t) int, user_data *void) {
	apu.dmc.rom_reader_data = user_data
	apu.dmc.rom_reader = reader
}

func (apu *APU) EndFrame(endTime cpu_time_t) {
	if endTime > apu.lastTime {
		apu.runUntil(endTime)
	}

	// make times relative to new frame
	apu.lastTime -= endTime
	require(apu.lastTime >= 0)

	if apu.nextIRQ != NESAPINoIRQ {
		apu.nextIRQ -= endTime
		assert(apu.nextIRQ >= 0)
	}

	if apu.dmc.nextIRQ != NESAPINoIRQ {
		apu.dmc.nextIRQ -= endTime
		assert(apu.dmc.nextIRQ >= 0)
	}

	if apu.earliestIRQ != NESAPINoIRQ {
		apu.earliestIRQ -= endTime
		if apu.earliestIRQ < 0 {
			apu.earliestIRQ = 0
		}
	}
}

var length_table = [0x20]unsigned_char{
	0x0A, 0xFE, 0x14, 0x02, 0x28, 0x04, 0x50, 0x06,
	0xA0, 0x08, 0x3C, 0x0A, 0x0E, 0x0C, 0x1A, 0x0E,
	0x0C, 0x10, 0x18, 0x12, 0x30, 0x14, 0x60, 0x16,
	0xC0, 0x18, 0x48, 0x1A, 0x10, 0x1C, 0x20, 0x1E,
}

func (apu *APU) WriteRegister(time cpu_time_t, addr cpu_addr_t, data int) {
	require(addr > 0x20) // addr must be actual address (i.e. 0x40xx)
	require(data <= 0xff && data >= 0)

	// Ignore addresses outside range
	if addr < NESAPUStartAddr || NESAPUEndAddr < addr {
		return
	}

	apu.runUntil(time)

	if addr < 0x4014 {
		// Write to channel
		var osc_index int = int((addr - NESAPUStartAddr) >> 2)
		osc := apu.oscs[osc_index]

		var reg int = int(addr & 3)
		osc.regs[reg] = unsigned_char(data) // WARNING(elemir): down conversion
		osc.reg_written[reg] = cbool.ToInt[unsigned_char](true)
		osc.ages[reg] = 0

		if osc_index == 4 {
			// handle DMC specially
			apu.dmc.write_register(reg, data)
		} else if reg == 3 {
			// load length counter
			if (apu.osc_enables>>osc_index)&1 != 0 {
				osc.length_counter = int(length_table[(data>>3)&0x1f])
			}

			// reset square phase
			if osc_index < 2 {
				// FIXME(elemir): convesion to square
				// ((Nes_Square*) osc)->phase = Nes_Square::phase_range - 1;
			}
		}
	} else if addr == 0x4015 {
		// Channel enables
		for i := NESAPUOscCount - 1; i >= 0; i-- {
			if (data>>i)&1 == 0 {
				apu.oscs[i].length_counter = 0
			}
		}

		var recalc_irq cbool.Bool = apu.dmc.irq_flag
		apu.dmc.irq_flag = true

		old_enables := apu.osc_enables
		apu.osc_enables = data

		if !cbool.FromInt(data & 0x10) {
			apu.dmc.nextIRQ = NESAPINoIRQ
			recalc_irq = true
		} else if !cbool.FromInt(old_enables & 0x10) {
			apu.dmc.start() // dmc just enabled
		}

		if recalc_irq {
			apu.irq_changed()
		}
	} else if addr == 0x4017 {
		// Frame mode
		apu.frameMode = data

		irq_enabled := !cbool.FromInt(data & 0x40)
		apu.irqFlag = irq_enabled && apu.irqFlag
		apu.nextIRQ = NESAPINoIRQ

		// mode 1
		apu.frameDelay = (apu.frameDelay & 1)
		apu.frame = 0

		if !cbool.FromInt(data & 0x80) {
			// mode 0
			apu.frame = 1
			apu.frameDelay += apu.framePeriod
			if irq_enabled {
				apu.nextIRQ = cpu_time_t(int(time) + apu.frameDelay + apu.framePeriod*3) // WARNING(elemir): downconversion
			}
		}

		apu.irq_changed()
	}
}

func (apu *APU) GetRegisterValues(regs *apu_register_values) {
	for i := range NESAPUOscCount {
		osc := apu.oscs[i]
		for j := range 4 {
			regs.regs[i*4+j] = osc.regs[j]
			regs.ages[i*4+j] = osc.ages[j]

			osc.ages[j] = increment_saturate(osc.ages[j])
		}

		regs.dpcm_bytes_left = unsigned_char(apu.dmc.length_counter)
		regs.dpcm_dac = unsigned_char(apu.dmc.last_amp)
	}
}

func (apu *APU) ReadStatus(time cpu_time_t) uint {
	var result uint
	apu.runUntil(time - 1)

	if apu.irqFlag {
		result = result | 1<<6
	}

	if apu.dmc.irq_flag {
		result = result | 1<<7
	}

	for i := range NESAPUOscCount {
		if apu.oscs[i].length_counter != 0 {
			result |= 1 << i
		}
	}
	apu.runUntil(time)

	if apu.irqFlag {
		apu.irqFlag = false
		apu.irq_changed()
	}

	return result
}

func (apu *APU) irq_changed() {
	var newIRQ cpu_time_t = apu.dmc.nextIRQ

	if apu.dmc.irq_flag || apu.irqFlag {
		newIRQ = 0
	} else if newIRQ > apu.nextIRQ {
		newIRQ = apu.nextIRQ
	}

	if newIRQ != apu.earliestIRQ {
		apu.earliestIRQ = newIRQ
		if apu.irqNotifier != nil {
			apu.irqNotifier(apu.irqData)
		}
	}
}

// frames

func (apu *APU) runUntil(end_time cpu_time_t) {
	require(end_time >= apu.lastTime)

	if end_time == apu.lastTime {
		return
	}

	fmt.Printf("runUntil %d %d\n", end_time, apu.lastTime)
	for {
		// earlier of next frame time or end time
		var time cpu_time_t = cpu_time_t(int(apu.lastTime) + apu.frameDelay) // WARNING(elemir): downconversion
		if time > end_time {
			time = end_time
		}
		apu.frameDelay -= int(time - apu.lastTime)

		fmt.Printf("runUntil last_time=%d frame_delay=%d time=%d\n", apu.lastTime, apu.frameDelay, time)

		// run oscs to present
		apu.square1.run(apu.lastTime, time)
		apu.square2.run(apu.lastTime, time)
		apu.triangle.run(apu.lastTime, time)
		apu.noise.run(apu.lastTime, time)
		apu.dmc.run(apu.lastTime, time)
		apu.lastTime = time

		if time == end_time {
			break // no more frames to run
		}

		// take frame-specific actions
		apu.frameDelay = apu.framePeriod
		apu.frame++
		switch apu.frame - 1 {
		case 0:
			if !cbool.FromInt(apu.frameMode & 0xc0) {
				apu.nextIRQ = cpu_time_t(int(time) + apu.framePeriod*4 + 1) // WARNING(elemir): downconversion
				apu.irqFlag = true
			}
			fallthrough
		case 2:
			// clock length and sweep on frames 0 and 2
			apu.square1.clock_length(0x20)
			apu.square2.clock_length(0x20)
			apu.noise.clock_length(0x20)
			apu.triangle.clock_length(0x80) // different bit for halt flag on triangle

			apu.square1.clock_sweep(-1)
			apu.square2.clock_sweep(0)
		case 1:
			// frame 1 is slightly shorter
			apu.frameDelay -= 2
		case 3:
			apu.frame = 0

			// frame 3 is almost twice as long in mode 1
			if !cbool.FromInt(apu.frameMode & 0x80) {
				apu.frameDelay += apu.framePeriod - 6
			}
		}

		// clock envelopes and linear counter every frame
		apu.triangle.clock_linear_counter()
		apu.square1.clock_envelope()
		apu.square2.clock_envelope()
		apu.noise.clock_envelope()
	}
}
