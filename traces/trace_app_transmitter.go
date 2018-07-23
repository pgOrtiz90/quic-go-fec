package traces//Defines the tracing levels

import (
	"os"
	"time"
	"fmt"
)

type trace_app_transmitter struct{

	// FILE
	FileName string
	file *os.File

	//Parameters of configuration
	delta float64
	target float64
	n_fec uint
	t_fec time.Duration

	//Results
	id 				uint
	tx_t			time.Duration   // Transmission completion time
	tx_bytes  int   // Amount of bytes Transmitted

	 lossRate float32
	 redundancy float32
}


func (t *trace_app_transmitter) OpenFile ( id uint, delta float64, target float64, N uint, T time.Duration){

	if _, err := os.Stat(t.FileName); os.IsNotExist(err) {
		t.file, _ = os.OpenFile(t.FileName, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
		fmt.Fprintf(t.file, " ID \t DELTA \t TARGET \t N \t T \t TX_TIME \t TX_BYTES \t LossRate \t Redundancy \n")
	} else {
		t.file, _ = os.OpenFile(t.FileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	}

	t.tx_t  = 0*time.Second
	t.tx_bytes = 0
	t.id = id

	t.delta = delta
	t.target = target
	t.n_fec = N
	t.t_fec = T
	t.lossRate = 0
	t.redundancy = 0

	return
}


func (t *trace_app_transmitter) Print (tx_t time.Duration, tx_bytes int){
	fmt.Fprintf(t.file, " %d \t  %f \t %f \t %d \t %f \t %f \t %d \t %f \t %f\n",  t.id, t.delta, t.target, t.n_fec, t.t_fec.Seconds(), tx_t.Seconds(), tx_bytes, t.lossRate, t.redundancy)
	t.lossRate = 0
	t.redundancy = 0
	t.id += 1
	return
}

func (t *trace_app_transmitter) close( ) {
	t.file.Close()
}

func (t *trace_app_transmitter) LossRateUpdate(lossRate float32){
	t.lossRate = lossRate
}

func (t *trace_app_transmitter) redundancyUpdate(redundancy float32){
	t.redundancy = redundancy
}