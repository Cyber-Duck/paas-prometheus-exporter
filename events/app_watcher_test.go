package events_test

import (
	"errors"
	"sync"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/ginkgo/extensions/table"

	"github.com/alphagov/paas-prometheus-exporter/events"
	"github.com/alphagov/paas-prometheus-exporter/events/mocks"
	"github.com/cloudfoundry-community/go-cfclient"
	sonde_events "github.com/cloudfoundry/sonde-go/events"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type FakeRegistry struct {
	mustRegisterCount int
	unregisterCount   int
	sync.Mutex
}

func (m *FakeRegistry) MustRegister(...prometheus.Collector) {
	m.Lock()
	defer m.Unlock()
	m.mustRegisterCount++
}

func (m *FakeRegistry) Register(prometheus.Collector) error {
	return errors.New("Not implemented")
}

func (m *FakeRegistry) Unregister(prometheus.Collector) bool {
	m.Lock()
	defer m.Unlock()
	m.unregisterCount++
	return true
}

func (m *FakeRegistry) MustRegisterCallCount() int {
	m.Lock()
	defer m.Unlock()
	return m.mustRegisterCount
}

func (m *FakeRegistry) UnregisterCallCount() int {
	m.Lock()
	defer m.Unlock()
	return m.unregisterCount
}

var _ = Describe("AppWatcher", func() {
	const METRICS_PER_INSTANCE = 8

	var (
		appWatcher     	         *events.AppWatcher
		registerer     	         *FakeRegistry
		streamProvider 	         *mocks.FakeAppStreamProvider
		closeAppWatcherAfterTest bool
		sondeEventChan           chan *sonde_events.Envelope
	)

	BeforeEach(func() {
		apps := []cfclient.App{
			{Guid: "33333333-3333-3333-3333-333333333333", Instances: 1, Name: "foo", SpaceURL: "/v2/spaces/123"},
		}

		registerer = &FakeRegistry{}
		streamProvider = &mocks.FakeAppStreamProvider{}
		sondeEventChan = make(chan *sonde_events.Envelope, 10)
		streamProvider.OpenStreamForReturns(sondeEventChan, nil)

		appWatcher, _ = events.NewAppWatcher(apps[0], registerer, streamProvider)
		closeAppWatcherAfterTest = true
	})

	AfterEach(func() {
		if closeAppWatcherAfterTest {
			appWatcher.Close()
		}
	})

	Describe("Run", func() {
		It("Registers metrics on startup", func() {
			Eventually(registerer.MustRegisterCallCount).Should(Equal(METRICS_PER_INSTANCE))
		})

		It("Unregisters metrics on close", func() {
			closeAppWatcherAfterTest = false

			appWatcher.Close()

			Eventually(registerer.UnregisterCallCount).Should(Equal(METRICS_PER_INSTANCE))
		})

		It("Registers more metrics when new instances are created", func() {
			Eventually(registerer.MustRegisterCallCount).Should(Equal(METRICS_PER_INSTANCE))

			appWatcher.UpdateAppInstances(2)

			Eventually(registerer.MustRegisterCallCount).Should(Equal(2 * METRICS_PER_INSTANCE))
		})

		It("Unregisters some metrics when old instances are deleted", func() {
			appWatcher.UpdateAppInstances(2)

			Eventually(registerer.MustRegisterCallCount).Should(Equal(2 * METRICS_PER_INSTANCE))

			appWatcher.UpdateAppInstances(1)

			Eventually(registerer.UnregisterCallCount).Should(Equal(METRICS_PER_INSTANCE))
		})

		It("sets container metrics for an instance", func() {
			var instanceIndex int32 = 0

			cpuPercentage := 10.0
			var diskBytes uint64 = 512
			var diskBytesQuota uint64 = 1024
			var memoryBytes uint64 = 1024
			var memoryBytesQuota uint64 = 4096

			containerMetric := sonde_events.ContainerMetric{
				CpuPercentage:    &cpuPercentage,
				DiskBytes:        &diskBytes,
				DiskBytesQuota:   &diskBytesQuota,
				InstanceIndex:    &instanceIndex,
				MemoryBytes:      &memoryBytes,
				MemoryBytesQuota: &memoryBytesQuota,
			}

			metricType := sonde_events.Envelope_ContainerMetric
			sondeEventChan <- &sonde_events.Envelope{ContainerMetric: &containerMetric, EventType: &metricType}

			cpuGauge := appWatcher.MetricsForInstance[instanceIndex].Cpu
			diskBytesGauge := appWatcher.MetricsForInstance[instanceIndex].DiskBytes
			diskUtilizationGauge := appWatcher.MetricsForInstance[instanceIndex].DiskUtilization
			memoryBytesGauge := appWatcher.MetricsForInstance[instanceIndex].MemoryBytes
			memoryUtilizationGauge := appWatcher.MetricsForInstance[instanceIndex].MemoryUtilization

			Eventually(func() float64 { return testutil.ToFloat64(cpuGauge) }).Should(Equal(cpuPercentage))
			Eventually(func() float64 { return testutil.ToFloat64(diskBytesGauge) }).Should(Equal(float64(diskBytes)))
			Eventually(func() float64 { return testutil.ToFloat64(memoryBytesGauge) }).Should(Equal(float64(memoryBytes)))

			// diskUtilization is a percentage based on diskBytes/diskBytesQuota*100 (512/1024*100 = 50)
			Eventually(func() float64 { return testutil.ToFloat64(diskUtilizationGauge) }).Should(Equal(float64(50)))

			// diskUtilization is a percentage based on memoryBytes/memoryBytesQuota*100 (1024/4096*100 = 25)
			Eventually(func() float64 { return testutil.ToFloat64(memoryUtilizationGauge) }).Should(Equal(float64(25)))
		})

		It("Increments the 'crashes' metric", func() {
			var instanceIndex int32 = 0
			envelopeLogMessageEventType := sonde_events.Envelope_LogMessage
			logMessageOutMessageType := sonde_events.LogMessage_OUT
			crashEnvelope := sonde_events.Envelope{
				Origin:    str("cloud_controller"),
				EventType: &envelopeLogMessageEventType,
				LogMessage: &sonde_events.LogMessage{
					Message:        []byte(fmt.Sprintf(
						"App instance exited with guid 4630f6ba-8ddc-41f1-afea-1905332d6660 payload: " +
						"{\"instance\"=>\"bc932892-f191-4fe2-60c3-7090\", \"index\"=>%d, \"reason\"=>\"CRASHED\"," +
						" \"exit_description\"=>\"APP/PROC/WEB: Exited with status 137\", \"crash_count\"=>1," +
						" \"crash_timestamp\"=>1512569260335558205, \"version\"=>\"d24b0422-0c88-4692-bf52-505091890e7d\"}",
						instanceIndex),
					),
					MessageType:    &logMessageOutMessageType,
					AppId:          str("4630f6ba-8ddc-41f1-afea-1905332d6660"),
					SourceType:     str("API"),
					SourceInstance: str("1"),
				},
			}

			crashCounter := &appWatcher.MetricsForInstance[instanceIndex].Crashes

			sondeEventChan <- &crashEnvelope
			Eventually(func() float64 { return testutil.ToFloat64(*crashCounter) }).Should(Equal(float64(1)))

			// Send another message to be extra confident that the behaviour is incremental
			sondeEventChan <- &crashEnvelope
			Eventually(func() float64 { return testutil.ToFloat64(*crashCounter) }).Should(Equal(float64(2)))
		})

		It("Does not increment the 'crashes' metric if not source type API, does not have App instance exited with guid, not LogMessage_OUT or not reason CRASHED", func() {
			var instanceIndex int32 = 0
			envelopeLogMessageEventType := sonde_events.Envelope_LogMessage
			logMessageOutMessageType := sonde_events.LogMessage_OUT
			logMessageErrMessageType := sonde_events.LogMessage_ERR

			appNonCrashEnvelopes := []sonde_events.Envelope{
				// source type not API
				sonde_events.Envelope{
					Origin:    str("gorouter"),
					EventType: &envelopeLogMessageEventType,
					LogMessage: &sonde_events.LogMessage{
						Message:        []byte("dora.dcarley.dev.cloudpipelineapps.digital - [2017-12-06T14:05:45.897+0000] \"GET / HTTP/1.1\" 200 0 13 \"-\" \"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/62.0.3202.94 Safari/537.36\" \"127.0.0.1:48966\" \"10.0.34.4:61223\" x_forwarded_for:\"213.86.153.212, 127.0.0.1\" x_forwarded_proto:\"https\" vcap_request_id:\"cd809903-c35d-4c98-6f62-1f22862cc82c\" response_time:0.018321645 app_id:\"4630f6ba-8ddc-41f1-afea-1905332d6660\" app_index:\"0\"\n"),
						MessageType:    &logMessageOutMessageType,
						AppId:          str("4630f6ba-8ddc-41f1-afea-1905332d6660"),
						SourceType:     str("RTR"),
						SourceInstance: str("1"),
					},
				},
				// log message type error
				sonde_events.Envelope{
					Origin:    str("rep"),
					EventType: &envelopeLogMessageEventType,
					LogMessage: &sonde_events.LogMessage{
						Message:        []byte("[2017-12-06 14:06:41] INFO  WEBrick 1.3.1"),
						MessageType:    &logMessageErrMessageType,
						AppId:          str("4630f6ba-8ddc-41f1-afea-1905332d6660"),
						SourceType:     str("APP/PROC/WEB"),
						SourceInstance: str("0"),
					},
				},
				// Does not start with App instance exited with guid
				sonde_events.Envelope{
					Origin:    str("cloud_controller"),
					EventType: &envelopeLogMessageEventType,
					LogMessage: &sonde_events.LogMessage{
						Message:        []byte("Updated app with guid 4630f6ba-8ddc-41f1-afea-1905332d6660 ({\"state\"=>\"STOPPED\"})"),
						MessageType:    &logMessageOutMessageType,
						AppId:          str("4630f6ba-8ddc-41f1-afea-1905332d6660"),
						SourceType:     str("API"),
						SourceInstance: str("1"),
					},
				},
				// no payload
				sonde_events.Envelope{
					Origin:    str("cloud_controller"),
					EventType: &envelopeLogMessageEventType,
					LogMessage: &sonde_events.LogMessage{
						Message:        []byte("Process has crashed with type: \"web\""),
						MessageType:    &logMessageOutMessageType,
						AppId:          str("4630f6ba-8ddc-41f1-afea-1905332d6660"),
						SourceType:     str("API"),
						SourceInstance: str("1"),
					},
				},
				// payload does not have CRASHED reason
				sonde_events.Envelope{
					Origin:    str("cloud_controller"),
					EventType: &envelopeLogMessageEventType,
					LogMessage: &sonde_events.LogMessage{
						Message:        []byte("Test without CRASHED payload: \"reason\"=>\"NOT_CRASHED\""),
						MessageType:    &logMessageOutMessageType,
						AppId:          str("4630f6ba-8ddc-41f1-afea-1905332d6660"),
						SourceType:     str("API"),
						SourceInstance: str("1"),
					},
				},
			}

			crashCounter := &appWatcher.MetricsForInstance[instanceIndex].Crashes

			for _, envelope := range appNonCrashEnvelopes {
				sondeEventChan <- &envelope
			}

			Consistently(func() float64 { return testutil.ToFloat64(*crashCounter) }).Should(Equal(float64(0)))
		})

		DescribeTable("increments the request metric for a given http status code range",
			func(statusRange string, statusCode int32) {
				// This test is currently limited to requests. Ideally it would also test the
				// response_time histogram but it's not possible to get private data out of
				// the histogram about its buckets and their values.

				startTimestamp := int64(0)
				stopTimestamp := int64(11 * time.Millisecond)
				clientPeerType := sonde_events.PeerType_Client
				getMethod := sonde_events.Method_GET
				instanceIndex := int32(0)
				envelopeHttpStartStopEventType := sonde_events.Envelope_HttpStartStop

				requestEnvelope := sonde_events.Envelope{
					EventType: &envelopeHttpStartStopEventType,
					HttpStartStop: &sonde_events.HttpStartStop{
						StartTimestamp: &startTimestamp,
						StopTimestamp:  &stopTimestamp,
						PeerType:       &clientPeerType,
						Method:         &getMethod,
						Uri:            str("/"),
						StatusCode:     &statusCode,
						InstanceIndex:  &instanceIndex,
					},
				}

				requestCounterVec := appWatcher.MetricsForInstance[instanceIndex].Requests
				requestCounter, _ := requestCounterVec.GetMetricWithLabelValues(statusRange)

				sondeEventChan <- &requestEnvelope
				Eventually(func() float64 { return testutil.ToFloat64(requestCounter) }).Should(Equal(float64(1)))

				// Send another event to be extra confident that the behaviour is incremental
				sondeEventChan <- &requestEnvelope
				Eventually(func() float64 { return testutil.ToFloat64(requestCounter) }).Should(Equal(float64(2)))
			},
			Entry("increments the 2xx request metric", "2xx", int32(226)),
			Entry("increments the 3xx request metric", "3xx", int32(302)),
			Entry("increments the 4xx request metric", "4xx", int32(418)),
			Entry("increments the 5xx request metric", "5xx", int32(507)),
		)

		// TODO: should we test the error paths?
		// how do we test them? As they are no handled but simply bubble up and then ignored
	})
})

func str(s string) *string {
	return &s
}
