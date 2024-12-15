# Sunlight Meter
Using the Adafruit TSL2591 Digital Light Sensor and a Raspberry Pi to monitor daily sunlight conditions.  

## About
The [TSL2591](https://www.adafruit.com/product/1980) is a high dynamic range digital light sensor.  
It can detect infrared, full-spectrum and human-visible light, then transmit that data over a serial connection.   

Features:  
- Monitors the current light conditions.
- Reports real-time data for monitoring or automation.
- Saves historical data to give insight on changes over time.
- Provides a dashboard to control the sensor and visualize data.
- Determines if your location is: ☁️ shade, partial shade, partial sun, or full sun ☀️

## How it works
### Configuration: 
The TSL2591 sensor is connected to a Raspberry Pi via i2c.  
Connecting the sensor to the Pi:
- Vin to 3.3V
- GND to GND
- SDA to SDA
- SCL to SCL

Sunlight Meter automatically adjusts sensor gain and integration time.  
This helps ensure accurate readings and avoid saturation in high light conditions.  

### API:
Sunlight Meter runs an API that allows remote access to the sensor data and jobs.  
Connect remotely to:
- Start/Stop any recording job.
- Receive real-time readings and light conditions. 
- Download historical data as a SQLite DB.
- Check device wifi-signal strength.

### Dashboard:
The dashboard is a web app that displays the current light conditions and historical data.  
- Visualize historical light conditions
- Control the sensor
- Export the results

## Understanding Lux Values
From https://en.wikipedia.org/wiki/Lux:  
| Illuminance (lux) | Surfaces illuminated by |
| --- | --- |
| 0.0001 | Moonless, overcast night sky (starlight) |
| 0.002 | Moonless clear night sky with airglow |
| 0.05–0.3 | Full moon on a clear night |
| 3.4 | Dark limit of civil twilight under a clear sky |
| 20–50 | Public areas with dark surroundings |
| 50 | Family living room lights |
| 80 | Office building hallway/toilet lighting |
| 100 | Very dark overcast day |
| 150 | Train station platforms |
| 320–500 | Office lighting |
| 400 | Sunrise or sunset on a clear day |
| 1000 | Overcast day; typical TV studio lighting |
| 10,000–25,000 | Full daylight (not direct sun) |
| 32,000–100,000 | Direct sunlight |