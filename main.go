package main

import (
	"encoding/binary"
	"fmt"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"hash/crc32"
	"io/ioutil"
	"os"
	"strconv"
	"time"
)

type Header struct {
	SampleRate uint32    `json:"sample_rate"`
	CenterFreq uint64    `json:"center_freq"`
	Timestamp  time.Time `json:"timestamp"`
	SampleSize uint32    `json:"sample_size"`
	Reserved   uint32    `json:"-"`
	CRC        uint32    `json:"crc"`
	CRCValid   bool      `json:"crc_valid"`
}

func (h *Header) String() string {
	return fmt.Sprintf("SampleRate: %d\n\rCenterFreq: %d\n\rTimestamp: %s\n\rSampleSize: %d\n\rCRC: %s",
		h.SampleRate, h.CenterFreq, h.Timestamp.String(), h.SampleSize, strconv.FormatBool(h.CRCValid))
}

func main() {
	// flags for input and output files using pFlags
	var input string
	var output string

	// parse flags
	flag.StringVar(&input, "input", "", "input file")
	flag.StringVar(&output, "output", "./raw", "output file")
	flag.Parse()

	// input flag is required
	if input == "" {
		logrus.Fatal("input file is required")
	}

	//bind flags to viper
	viper.BindPFlag("input", flag.Lookup("input"))
	viper.BindPFlag("output", flag.Lookup("output"))

	// read file in input
	file, err := os.OpenFile(viper.GetString("input"), os.O_RDONLY, 0644)
	if err != nil {
		logrus.WithError(err).Fatal("error opening file")
	}

	// read file and save the content in a variable
	content, err := ioutil.ReadAll(file)
	if err != nil {
		logrus.WithError(err).Fatal("error reading file")
	}

	// extract header, first 32 bytes
	header := content[:32]

	// fix header slice into Header struct
	var h Header
	h.SampleRate = binary.LittleEndian.Uint32(header[0:4])
	h.CenterFreq = binary.LittleEndian.Uint64(header[4:12])
	timestamp := binary.LittleEndian.Uint64(header[12:20])
	h.SampleSize = binary.LittleEndian.Uint32(header[20:24])
	h.Reserved = binary.LittleEndian.Uint32(header[24:28])
	h.CRC = binary.LittleEndian.Uint32(header[28:32])

	// calc crc
	crc := crc32.ChecksumIEEE(header[:28])

	// check crc
	h.CRCValid = crc == h.CRC
	if !h.CRCValid {
		logrus.Info("CRC mismatch")
	}

	// convert timestamp to time.Time
	h.Timestamp = time.UnixMilli(int64(timestamp))

	// print header
	fmt.Println(h.String())

	// write header to human-readable file
	err = ioutil.WriteFile(viper.GetString("output")+"-info.txt", []byte(h.String()), 0644)
	if err != nil {
		logrus.WithError(err).Fatal("error writing file")
	}

	sampleRate := h.SampleRate
	sampleRateCalc := (sampleRate * 16 * 2) / 8

	// sample rate to byte slice
	sampleRateBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sampleRateBytes, uint32(sampleRate))

	// sample rate calc to byte slice
	sampleRateCalcBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sampleRateCalcBytes, uint32(sampleRateCalc))

	waveHeader := []byte{
		'R', 'I', 'F', 'F',
		0, 0, 0, 0,
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ',
		16, 0, 0, 0,
		1, 0,
		2, 0,
		sampleRateBytes[0], sampleRateBytes[1], sampleRateBytes[2], sampleRateBytes[3],
		sampleRateCalcBytes[0], sampleRateCalcBytes[1], sampleRateCalcBytes[2], sampleRateCalcBytes[3],
		4, 0,
		16, 0,
		'd', 'a', 't', 'a',
		0, 0, 0, 0,
	}

	// write wave header to file
	body := waveHeader

	//convert body to []int64
	//var samples = make([]uint32, len(content[32:])/4)
	startIndex := 32
	for i := 0; i < len(content[startIndex:])/4; i++ {
		index := startIndex + (i * 4)
		sample := int32(binary.LittleEndian.Uint32(content[index:]))
		sample = sample << 8
		sample = sample >> 16

		sampleBytes := make([]byte, 2)
		binary.LittleEndian.PutUint16(sampleBytes, uint16(sample))
		body = append(body, sampleBytes...)
	}

	bodySamples := body[44:]
	bodySamplesLen := len(bodySamples) / 2

	logrus.Info("body samples length: ", bodySamplesLen%8)

	//body = append(body, convertTo16Bit(binary.LittleEndian.Uint32(content[32:]))...)
	// calc file size
	fileSize := len(body) - 8

	// convert file size to byte slice
	fileSizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(fileSizeBytes, uint32(fileSize))

	// write file size to wave header
	body[4] = fileSizeBytes[0]
	body[5] = fileSizeBytes[1]
	body[6] = fileSizeBytes[2]
	body[7] = fileSizeBytes[3]

	// calc data size
	dataSize := len(body) - 44

	// convert data size to byte slice
	dataSizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(dataSizeBytes, uint32(dataSize))

	// write data size to wave header
	body[40] = dataSizeBytes[0]
	body[41] = dataSizeBytes[1]
	body[42] = dataSizeBytes[2]
	body[43] = dataSizeBytes[3]

	// write body to file
	err = ioutil.WriteFile(viper.GetString("output")+"-iq.wav", body, 0644)
	if err != nil {
		logrus.WithError(err).Fatal("error writing file")
	}

	// close file
	err = file.Close()
	if err != nil {
		logrus.WithError(err).Fatal("error closing file")
	}

	// print success
	logrus.Info("done")

	// exit
	os.Exit(0)
}

/**
 * Converts 32-bit samples into a 16-bit samples array
 */
func convertTo16Bit(samples []uint32) []byte {
	var result = make([]int16, len(samples))

	for i, sample := range samples {
		result[i] = int16(sample >> 16)
	}

	return convertToByte(result)
}

/**
 * Converts 16-bit samples into a byte array
 */
func convertToByte(samples []int16) []byte {
	var result = make([]byte, len(samples)*2)

	for i, sample := range samples {
		result[i*2] = byte(sample)
		result[i*2+1] = byte(sample >> 8)
	}

	return result
}
