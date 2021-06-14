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

      # backfill information for naras basd on neighbours in case there's something
      @db.dup.each do |n, entry|
        observations = entry["Observations"]
        observations.each do |nn, o|
          @db[nn] ||= {}
          @db[nn]["Name"] ||= nn
          #@db[nn]["Barrio"] ||= o["ClusterName"]
          @db[nn]["LastSeen"] ||= o["LastSeen"]
          @db[nn]["Observations"] ||= {}
          @db[nn]["Observations"][nn] = o.dup.merge((@db[nn]["Observations"][nn] || {}).compact)
          @db[nn]["HostStats"] ||= { "Uptime" => 0 }
        end
      end

      @db.each do |n, entry|
        observations = entry["Observations"]
        time_last_seen = Time.now.to_i - entry["LastSeen"]
        if time_last_seen > 60 && observations[n]["Online"] == "ONLINE"
          observations[n]["Online"] = "MISSING"
        end
      end

      @db = @db.to_a.sort_by { |name, data| [data.fetch("Flair", ""), name] }.to_h
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
    else
      name = topic.split("/").last
      status = message
    end

    status["Name"] = name
    status["LastSeen"] = Time.now.to_i
    # when naras boot they're observations are weak
    status["Observations"][name] ||= {}
    status["Observations"][name]["LastSeen"] = Time.now.to_i
    status["Observations"][name]["LastRestart"] ||= Time.now.to_i
    if topic =~ /chau/
      status["Observations"][name]["Online"] = "OFFLINE"
    else
      status["Observations"][name]["Online"] = "ONLINE"
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
