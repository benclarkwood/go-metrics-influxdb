package influxdb

import (
	"fmt"
	"log"
	uurl "net/url"
	"time"

	"os"

	"github.com/influxdata/influxdb/client"
	"github.com/rcrowley/go-metrics"
)

type reporter struct {
	reg      metrics.Registry
	interval time.Duration

	tagHost bool

	url      uurl.URL
	database string
	username string
	password string

	client *client.Client
}

// InfluxDB starts a InfluxDB reporter which will post the metrics from the given registry at each d interval.
func InfluxDB(r metrics.Registry, d time.Duration, url, database, username, password string, tagHost bool) {
	u, err := uurl.Parse(url)
	if err != nil {
		log.Printf("unable to parse InfluxDB url %s. err=%v", url, err)
		return
	}

	rep := &reporter{
		reg:      r,
		interval: d,
		tagHost:  tagHost,
		url:      *u,
		database: database,
		username: username,
		password: password,
	}
	if err := rep.makeClient(); err != nil {
		log.Printf("unable to make InfluxDB client. err=%v", err)
		return
	}

	rep.run()
}

func (r *reporter) makeClient() (err error) {
	r.client, err = client.NewClient(client.Config{
		URL:      r.url,
		Username: r.username,
		Password: r.password,
	})

	return
}

func (r *reporter) run() {
	intervalTicker := time.Tick(r.interval)
	pingTicker := time.Tick(time.Second * 5)

	for {
		select {
		case <-intervalTicker:
			if err := r.send(); err != nil {
				log.Printf("unable to send metrics to InfluxDB. err=%v", err)
			}
		case <-pingTicker:
			_, _, err := r.client.Ping()
			if err != nil {
				log.Printf("got error while sending a ping to InfluxDB, trying to recreate client. err=%v", err)

				if err = r.makeClient(); err != nil {
					log.Printf("unable to make InfluxDB client. err=%v", err)
				}
			}
		}
	}
}

func (r *reporter) send() error {
	var pts []client.Point

	host := ""

	if r.tagHost {
		hostName, err := os.Hostname()
		if err != nil {
			return err
		}

		host = hostName + "."
	}

	r.reg.Each(func(name string, i interface{}) {
		now := time.Now()

		// Prefix the namespace with the host
		name = host + name

		switch m := i.(type) {
		case metrics.Counter:
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.count", name),
				Fields: map[string]interface{}{
					"value": m.Count(),
				},
				Time: now,
			})
		case metrics.Gauge:
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.gauge", name),
				Fields: map[string]interface{}{
					"value": m.Value(),
				},
				Time: now,
			})
		case metrics.GaugeFloat64:
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.gauge", name),
				Fields: map[string]interface{}{
					"value": m.Value(),
				},
				Time: now,
			})
		case metrics.Histogram:
			ps := m.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.histogram", name),
				Fields: map[string]interface{}{
					"count":    m.Count(),
					"max":      m.Max(),
					"mean":     m.Mean(),
					"min":      m.Min(),
					"stddev":   m.StdDev(),
					"variance": m.Variance(),
					"p50":      ps[0],
					"p75":      ps[1],
					"p95":      ps[2],
					"p99":      ps[3],
					"p999":     ps[4],
					"p9999":    ps[5],
				},
				Time: now,
			})
		case metrics.Meter:
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.meter", name),
				Fields: map[string]interface{}{
					"count": m.Count(),
					"m1":    m.Rate1(),
					"m5":    m.Rate5(),
					"m15":   m.Rate15(),
					"mean":  m.RateMean(),
				},
				Time: now,
			})
		case metrics.Timer:
			ps := m.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			pts = append(pts, client.Point{
				Measurement: fmt.Sprintf("%s.timer", name),
				Fields: map[string]interface{}{
					"count":    m.Count(),
					"max":      m.Max() / time.Millisecond.Nanoseconds(),               // ms time
					"mean":     m.Mean() / float64(time.Millisecond.Nanoseconds()),     // ms time
					"min":      m.Min() / time.Millisecond.Nanoseconds(),               // ms time
					"stddev":   m.StdDev() / float64(time.Millisecond.Nanoseconds()),   // ms time
					"variance": m.Variance() / float64(time.Millisecond.Nanoseconds()), // ms time
					"p50":      ps[0] / float64(time.Millisecond.Nanoseconds()),        // ms time
					"p75":      ps[1] / float64(time.Millisecond.Nanoseconds()),        // ms time
					"p95":      ps[2] / float64(time.Millisecond.Nanoseconds()),        // ms time
					"p99":      ps[3] / float64(time.Millisecond.Nanoseconds()),        // ms time
					"p999":     ps[4] / float64(time.Millisecond.Nanoseconds()),        // ms time
					"p9999":    ps[5] / float64(time.Millisecond.Nanoseconds()),        // ms time
					"m1":       m.Rate1(),
					"m5":       m.Rate5(),
					"m15":      m.Rate15(),
					"meanrate": m.RateMean(),
				},
				Time: now,
			})
		}
	})

	bps := client.BatchPoints{
		Points:   pts,
		Database: r.database,
	}

	_, err := r.client.Write(bps)
	return err
}
