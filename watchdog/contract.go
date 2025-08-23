package watchdog

type LeaderElectingWatchdog interface {
	Elect(cfg Config) <-chan int
	Stop()
}
