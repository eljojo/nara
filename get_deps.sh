echo "[$(hostname)] getting dependencies"
go get github.com/eclipse/paho.mqtt.golang
go get github.com/sirupsen/logrus
go get github.com/go-ping/ping
go get -u github.com/shirou/gopsutil/host
go get -u github.com/shirou/gopsutil/load
go get github.com/lensesio/tableprinter
go get -u golang.org/x/sys/unix
