echo "[$(hostname)] getting dependencies"
go get github.com/eclipse/paho.mqtt.golang
go get github.com/sirupsen/logrus
go get github.com/sparrc/go-ping
go get -u github.com/shirou/gopsutil/host
go get -u github.com/shirou/gopsutil/load
go get github.com/lensesio/tableprinter
