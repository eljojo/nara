require 'mqtt'
require 'json'
require 'logger'
require 'sinatra'
require "sinatra/json"
require 'socket'

Thread.abort_on_exception = true

class NaraWeb
  def self.hostname
    @hostname ||= Socket.gethostname
  end

  def self.production?
    return @prod if defined?(@prod)
    @prod = (ENV['RACK_ENV'] == "production")
  end
  attr_reader :db

  def initialize
    @client = MqttClient.new
    @db = {}
  end

  def start!
    loop do
      name, status = @client.fetch
      next unless name

      @db[name] = status
      @db[name]["Name"] = name
      @db[name]["LastSeen"] = Time.now.to_i

      @db.dup.each do |n, entry|
        observations = entry["Observations"]
        observations.each do |nn, o|
          next if @db.key?(nn)
          @db[nn] = {}
          @db[nn]["Name"] = nn
          @db[nn]["Barrio"] = o["ClusterName"]
          @db[nn]["LastSeen"] = o["LastSeen"]
          @db[nn]["Observations"] = { nn => o.dup }
          @db[nn]["HostStats"] = { "Uptime" => 0 }
        end
      end

      @db.each do |n, entry|
        observations = entry["Observations"]
        time_last_seen = Time.now.to_i - entry["LastSeen"]
        if time_last_seen > 60 && observations[n]["Online"] == "ONLINE"
          observations[n]["Online"] = "MISSING"
        end
      end

      @db = @db.to_a.sort_by { |name, data| [data.fetch("Barrio", name), name] }.to_h
    end
  end
end

class MqttClient
  def fetch
    topic, message_json = client.get
    message = JSON.parse(message_json)
    if topic =~ /nara\/plaza/
      name = message["Name"]
      status = message.fetch("Status")
      puts status
    else
      name = topic.split("/").last
      status = message
    end
    [name, status]
  rescue MQTT::ProtocolException, SocketError, Errno::ECONNREFUSED
    disconnect
    sleep 1
    [nil, nil]
  end

  def disconnect
    @client&.disconnect
    @client = nil
  end

  private

  def client
    return @client if @client
    @client = MQTT::Client.connect(MQTT_CONN)
    $log.info("connected to MQTT server")
    @client.subscribe('nara/newspaper/#')
    @client.subscribe('nara/plaza/#')
    @client
  end
end

MQTT_CONN = { username: ENV.fetch('MQTT_USER'), password: ENV.fetch('MQTT_PASS'), host: ENV.fetch('MQTT_HOST', 'hass.eljojo.casa'), ssl: true }

$log = Logger.new(STDOUT)
$log.level = if NaraWeb.production? then Logger::INFO else Logger::DEBUG end
naraweb = NaraWeb.new

Thread.new { naraweb.start! }

get '/' do
  erb :index
end

get '/api.json' do
  json({ naras: naraweb.db.values, server: NaraWeb.hostname })
end
