package monitor

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	. "m7s.live/engine/v4"
	"m7s.live/engine/v4/common"
	"m7s.live/engine/v4/config"
	// badger "github.com/dgraph-io/badger/v3"
)

type Index struct {
	CreateTime int64  `yaml:"time"`
	StreamPath string `yaml:"path"`
}

type DelaySnap struct {
	Time  int64             `yaml:"time"`
	Delay map[string]uint32 `yaml:"delay"`
}

type TrackSnap struct {
	Time   int64 `yaml:"time"`
	BPS    int   `yaml:"bps"`
	FPS    int   `yaml:"fps"`
	Drops  int   `yaml:"drops"`
	RBSize int   `yaml:"rb"`
}

type MonitorSuber struct {
	Subscriber
	subIndex int
	subfp    map[ISubscriber]*os.File
	fp       *os.File
	tracks   map[common.Track]*os.File
	dir      string
}

func (r *MonitorSuber) OnEvent(event any) {
	switch v := event.(type) {
	case PulseEvent:
		r.Stream.Tracks.Range(func(k string, t common.Track) {
			track := t.GetBase()
			appendYaml(r.tracks[t], TrackSnap{
				Time:   v.Time.UnixMilli(),
				BPS:    track.BPS,
				FPS:    track.FPS,
				Drops:  track.Drops,
				RBSize: t.GetRBSize(),
			})
		})
		r.Stream.Subscribers.RangeAll(func(sub ISubscriber) {
			var snap = DelaySnap{
				Time:  v.Time.UnixMilli(),
				Delay: map[string]uint32{},
			}
			suber := sub.GetSubscriber()
			if suber.AudioReader.Track != nil {
				snap.Delay[suber.AudioReader.Track.GetBase().Name] = suber.AudioReader.Delay
			}
			if suber.VideoReader.Track != nil {
				snap.Delay[suber.VideoReader.Track.GetBase().Name] = suber.VideoReader.Delay
			}
			appendYaml(r.subfp[sub], snap)
		})
	case common.Track:
		r.tracks[v] = conf.OpenYaml(r.dir, "track", v.GetBase().Name)
		// appendYaml(r.fp, map[string]any{"time": time.Now().UnixMilli(), "event": "publish", "type": v.Target.GetType()})
	}
}

type MonitorConfig struct {
	config.HTTP
	config.Subscribe
	Path       string `default:"monitor"` // 存储路径
	indexFP    *os.File
	fileServer http.Handler
	today      string
}

var conf MonitorConfig
var MonitorPlugin = InstallPlugin(&conf)
var streams map[*Stream]*MonitorSuber

func (conf *MonitorConfig) OnEvent(event any) {
	switch v := event.(type) {
	case FirstConfig:
		streams = make(map[*Stream]*MonitorSuber)
		conf.fileServer = http.FileServer(http.Dir(conf.Path))
	case SEcreate:
		// conf.Report("create", &ReportCreateStream{v.Target.Path, time.Now().Unix()})
		var suber MonitorSuber
		streams[v.Target] = &suber
		if today := time.Now().Format("2006-01-02"); conf.indexFP == nil {
			conf.indexFP = conf.OpenYaml(today)
			conf.today = today
		} else if conf.today != today {
			conf.indexFP.Close()
			conf.indexFP = conf.OpenYaml(today)
			conf.today = today
		}
		appendYaml(conf.indexFP, Index{CreateTime: v.Time.UnixMilli(), StreamPath: v.Target.Path})
		suber.dir = filepath.Join(v.Target.Path, time.Now().Format("2006-01-02T15:04:05"))
		suber.fp = conf.OpenYaml(suber.dir, "stream")
		suber.subfp = make(map[ISubscriber]*os.File)
		suber.tracks = make(map[common.Track]*os.File)
		suber.IsInternal = true
		if Engine.Subscribe(v.Target.Path, &suber) == nil {
			suber.SubPulse()
		}
	case SEpublish:
		appendYaml(streams[v.Target].fp, map[string]any{"time": v.Time.UnixMilli(), "event": "publish", "type": v.Target.GetType()})
	case SErepublish:
		appendYaml(streams[v.Target].fp, map[string]any{"time": v.Time.UnixMilli(), "event": "republish", "type": v.Target.GetType()})
	case SEclose:
		monitor := streams[v.Target]
		appendYaml(monitor.fp, map[string]any{"time": v.Time.UnixMilli(), "event": "close", "action": v.Action})
	case SEwaitClose:
		monitor := streams[v.Target]
		appendYaml(monitor.fp, map[string]any{"time": v.Time.UnixMilli(), "event": "waitclose", "action": v.Action})
	case SEwaitPublish:
		monitor := streams[v.Target]
		appendYaml(monitor.fp, map[string]any{"time": v.Time.UnixMilli(), "event": "waitpublish", "action": v.Action})
	case ISubscriber:
		suber := v.GetSubscriber()
		monitor := streams[suber.Stream]
		monitor.subIndex++
		monitor.subfp[v] = conf.OpenYaml(monitor.dir, "subscriber", fmt.Sprintf("%d", monitor.subIndex))
		appendYaml(monitor.fp, map[string]any{"time": time.Now().UnixMilli(), "event": "subscribe", "type": suber.Type, "id": suber.ID})
	case UnsubscribeEvent:
		s := v.Target.GetSubscriber().Stream
		monitor := streams[s]
		monitor.subfp[v.Target].Close()
		appendYaml(monitor.fp, map[string]any{"time": v.Time.UnixMilli(), "event": "unsubscribe"})
	}
}

func (conf *MonitorConfig) API_list_stream(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var streams []Index
	timeRange := query.Get("time")
	streamPath := query.Get("streamPath")
	if timeRange == "" {
		conf.ReadYaml(&streams, time.Now().Format("2006-01-02"))
	} else if tt := strings.Split(timeRange, "-"); len(tt) == 2 {
		t, _ := strconv.ParseInt(tt[0], 10, 64)
		start := time.UnixMilli(t)
		t, _ = strconv.ParseInt(tt[1], 10, 64)
		end := time.UnixMilli(t)
		for ; !start.After(end); start = start.AddDate(0, 0, 1) {
			var s []Index
			conf.ReadYaml(&s, start.Format("2006-01-02"))
			if streamPath != "" {
				for _, v := range s {
					if v.StreamPath == streamPath {
						streams = append(streams, v)
					}
				}
			} else {
				streams = append(streams, s...)
			}
		}
	}
	if err := yaml.NewEncoder(w).Encode(streams); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (conf *MonitorConfig) API_list_track(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	streamPath := query.Get("streamPath")
	var tracks []string
	filepath.Walk(filepath.Join(conf.Path, streamPath, "track"), func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		trackName, _, _ := strings.Cut(info.Name(), ".")
		tracks = append(tracks, trackName)
		return nil
	})
	if err := yaml.NewEncoder(w).Encode(tracks); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (conf *MonitorConfig) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conf.fileServer.ServeHTTP(w, r)
}

func appendYaml[T any](fp *os.File, data T) {
	out, _ := yaml.Marshal([]T{data})
	fp.Write(out)
}

func (conf *MonitorConfig) OpenYaml(path ...string) (f *os.File) {
	fp := filepath.Join(append([]string{conf.Path}, path...)...) + ".yaml"
	os.MkdirAll(filepath.Dir(fp), 0766)
	f, _ = os.OpenFile(fp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	return
}

func (conf *MonitorConfig) ReadYaml(data any, path ...string) {
	f, _ := os.Open(filepath.Join(append([]string{conf.Path}, path...)...) + ".yaml")
	yaml.NewDecoder(f).Decode(data)
}
