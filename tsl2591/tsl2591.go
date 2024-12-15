package tsl2591

/*
 * tsl2591 - Package for interacting with TSL2591 lux sensors.
 *
 * Ref:
 * https://github.com/adafruit/Adafruit_TSL2591_Library
 * https://github.com/mstahl/tsl2591
 *
 */

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/exp/io/i2c"
)

var l *logrus.Logger

func init() {
	l = logrus.New()
	// Setup the logger, so it can be parsed by datadog
	l.Formatter = &logrus.JSONFormatter{}
	l.SetOutput(os.Stdout)
	// Set the log level
	logLevel := strings.ToLower(os.Getenv("LOG_LEVEL"))
	switch logLevel {
	case "debug":
		l.SetLevel(logrus.DebugLevel)
	case "info":
		l.SetLevel(logrus.InfoLevel)
	case "error":
		l.SetLevel(logrus.ErrorLevel)
	default:
		l.SetLevel(logrus.InfoLevel)
	}
}

type TSL2591 struct {
	Enabled bool
	Timing  byte
	Gain    byte
	Device  *i2c.Device
	*sync.Mutex
}

// Connect to a TSL2591 via I2C protocol & set gain/timing
func NewTSL2591(gain byte, timing byte, path string) (*TSL2591, error) {
	if path == "" {
		// i2c-1 is the default I2C bus for the Raspberry Pi
		path = "/dev/i2c-1"
	}
	device, err := i2c.Open(&i2c.Devfs{Dev: path}, int(TSL2591_ADDR))
	if err != nil {
		return nil, fmt.Errorf("Failed to open: %w", err)
	}
	tsl := &TSL2591{
		Device:  device,
		Mutex:   &sync.Mutex{},
		Enabled: true,
	}

	// Read the device ID from the TSL2591
	buf := make([]byte, 1)
	err = tsl.Device.ReadReg(TSL2591_COMMAND_BIT|TSL2591_REGISTER_DEVICE_ID, buf)
	if err != nil {
		return nil, fmt.Errorf("Failed to read ref: %w", err)
	}
	if buf[0] != 0x50 {
		return nil, errors.New("Can't find a TSL2591 on I2C bus /dev/i2c-1")
	}

	tsl.SetTiming(timing)
	tsl.SetGain(gain)

	tsl.Disable()
	return tsl, nil
}

// Read from the light sensor's channels
func (tsl *TSL2591) GetFullLuminosity() (uint16, uint16, error) {
	if !tsl.Enabled {
		return 0, 0, errors.New("sensor must be enabled")
	}

	for d := byte(0); d < tsl.Timing; d++ {
		time.Sleep(200 * time.Millisecond)
	}

	// Reading from TSL2591_REGISTER_CHAN0_LOW, and TSL2591_REGISTER_CHAN1_LOW
	// They are 2 bytes each, so we read 4 bytes in total
	bytes := make([]byte, 4)
	err := tsl.Device.ReadReg(TSL2591_COMMAND_BIT|TSL2591_REGISTER_CHAN0_LOW, bytes)
	if err != nil {

		fmt.Printf("Error reading from register: %v\n", err)
		return 0, 0, err
	}
	l.Debugf("Bytes read: %v\n", bytes)

	channel0 := binary.LittleEndian.Uint16(bytes[0:])
	channel1 := binary.LittleEndian.Uint16(bytes[2:])

	l.Debugf("Channel 0: %v, Channel 1: %v\n", channel0, channel1)
	return channel0, channel1, nil
}

func (tsl *TSL2591) CalculateLux(ch0, ch1 uint16) (float64, error) {
	// Check for channel overflow
	if ch0 == 0xFFFF || ch1 == 0xFFFF {
		return 0, fmt.Errorf("Overflow: Channel 0: %v, Channel 1: %v\n", ch0, ch1)
	}

	var int_time float64
	switch tsl.Timing {
	case TSL2591_INTEGRATIONTIME_100MS:
		int_time = 100.0
	case TSL2591_INTEGRATIONTIME_200MS:
		int_time = 200.0
	case TSL2591_INTEGRATIONTIME_300MS:
		int_time = 300.0
	case TSL2591_INTEGRATIONTIME_400MS:
		int_time = 400.0
	case TSL2591_INTEGRATIONTIME_500MS:
		int_time = 500.0
	case TSL2591_INTEGRATIONTIME_600MS:
		int_time = 600.0
	default:
		int_time = 100.0
	}

	var adj_gain float64
	switch tsl.Gain {
	case TSL2591_GAIN_LOW:
		adj_gain = 1.0
	case TSL2591_GAIN_MED:
		adj_gain = 25.0
	case TSL2591_GAIN_HIGH:
		adj_gain = 428.0
	case TSL2591_GAIN_MAX:
		adj_gain = 9876.0
	default:
		adj_gain = 1.0
	}

	// Based on the formula provided in the datasheet of the TSL2591 sensor
	cpl := (int_time * adj_gain) / TSL2591_LUX_DF
	lux := (float64(ch0) - float64(ch1)) * (1.0 - (float64(ch1) / float64(ch0))) / cpl
	return lux, nil
}

func (tsl *TSL2591) SetOptimalGain() error {
	// Try each gain option and see if the sensor is saturated
	gainOptions := []byte{TSL2591_GAIN_LOW, TSL2591_GAIN_MED, TSL2591_GAIN_HIGH, TSL2591_GAIN_MAX}
	integrationOptions := []byte{TSL2591_INTEGRATIONTIME_600MS, TSL2591_INTEGRATIONTIME_500MS, TSL2591_INTEGRATIONTIME_400MS, TSL2591_INTEGRATIONTIME_300MS, TSL2591_INTEGRATIONTIME_200MS, TSL2591_INTEGRATIONTIME_100MS}
	for _, gain := range gainOptions {
		tsl.SetGain(gain)
		for _, time := range integrationOptions {
			tsl.SetTiming(time)
			l.Debugf("Attempting - Gain: %v, Integration Time: %v", GainToString(gain), IntegrationTimeToString(time))
			ch0, ch1, err := tsl.GetFullLuminosity()
			if err != nil {
				continue
			}
			if ch0 == 0xFFFF || ch1 == 0xFFFF {
				continue
			}
			lux, err := tsl.CalculateLux(ch0, ch1)
			if err != nil {
				continue
			} else if lux == 0 {
				continue
			}
			l.Debugf("Set - Gain: %v, Integration Time: %v", GainToString(gain), IntegrationTimeToString(time))
			return nil
		}
	}
	// Use default options
	tsl.SetGain(TSL2591_GAIN_LOW)
	tsl.SetTiming(TSL2591_INTEGRATIONTIME_600MS)
	return errors.New("All gain options are saturated")
}

// Returns the normalized output for a given spectrum type
func GetNormalizedOutput(spectrumType byte, ch0, ch1 uint16) float64 {
	switch spectrumType {
	case TSL2591_VISIBLE:
		visible := float64(ch0) - float64(ch1)
		if visible < 0 {
			visible = 0
		}
		return visible / 0xFFFF
	case TSL2591_INFRARED:
		return float64(ch1) / 0xFFFF
	case TSL2591_FULLSPECTRUM:
		return float64(ch0) / 0xFFFF
	default:
		return 0
	}
}

// Enable the sensor
func (tsl *TSL2591) Enable() error {
	tsl.Lock()
	defer tsl.Unlock()

	if tsl.Enabled {
		return nil
	}
	var write []byte = []byte{
		TSL2591_ENABLE_POWERON | TSL2591_ENABLE_AEN | TSL2591_ENABLE_AIEN | TSL2591_ENABLE_NPIEN,
	}
	if err := tsl.Device.WriteReg(TSL2591_COMMAND_BIT|TSL2591_REGISTER_ENABLE, write); err != nil {
		return err
	}
	tsl.Enabled = true
	return nil
}

// Disable the sensor
func (tsl *TSL2591) Disable() error {
	tsl.Lock()
	defer tsl.Unlock()

	if !tsl.Enabled {
		return nil
	}
	var write []byte = []byte{
		TSL2591_ENABLE_POWEROFF,
	}
	if err := tsl.Device.WriteReg(TSL2591_COMMAND_BIT|TSL2591_REGISTER_ENABLE, write); err != nil {
		return err
	}
	tsl.Enabled = false
	return nil
}

// Set the gain for the sensor
func (tsl *TSL2591) SetGain(gain byte) error {
	if !tsl.Enabled {
		return errors.New("sensor must be enabled")
	}

	write := []byte{
		tsl.Timing | gain,
	}
	if err := tsl.Device.WriteReg(TSL2591_COMMAND_BIT|TSL2591_REGISTER_CONTROL, write); err != nil {
		return err
	}
	tsl.Gain = gain
	return nil
}

// Set the integration timing for the sensor
func (tsl *TSL2591) SetTiming(timing byte) error {
	if !tsl.Enabled {
		return errors.New("sensor must be enabled")
	}

	write := []byte{
		timing | tsl.Gain,
	}
	err := tsl.Device.WriteReg(TSL2591_COMMAND_BIT|TSL2591_REGISTER_CONTROL, write)
	if err != nil {
		return err
	}
	tsl.Timing = timing
	return nil
}
