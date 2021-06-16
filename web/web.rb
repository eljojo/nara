require 'mqtt'
require 'json'
require 'logger'
require 'sinatra'
require "sinatra/json"
require 'socket'
require 'time'

Thread.abort_on_exception = true

class Nara
  attr_reader :name, :status

  def initialize(name)
    @name = name
    @status = {}
    @last_seen = nil
  end

  def to_h
    last_restart = self_opinion.fetch("LastRestart", 0)
    last_restart = Time.now.to_i if last_restart == 0

    start_time = self_opinion.fetch("StartTime", 0)
    start_time = Time.now if start_time == 0
    {
      Name: name,
      Flair: status.fetch("Flair", ""),
      Buzz: status.fetch("Buzz", 0),
      Chattiness: status.fetch("Chattiness", 0),
      LastSeen: last_seen.to_i,
      LastRestart: last_restart,
      Online: self_opinion.fetch("Online", "?"),
      StartTime: Time.at(start_time).to_i,
      Restarts: self_opinion.fetch("Restarts", 0),
      Uptime: status.dig("HostStats", "Uptime") || 0
    }
  end

  def mark_as_seen
    @last_seen = Time.now
  end

  def status=(new_status)
    @status = new_status
  end

  def last_seen
    return @last_seen if @last_seen
    opinion = self_opinion.fetch("LastSeen", 0)
    return if opinion == 0
    Time.at(opinion)
  end

  def mark_if_missing
    time_last_seen = Time.now.to_i - last_seen.to_i
    if time_last_seen > 60 && online?
      observations[name]["Online"] = "MISSING"
    end
  end

  def online?
    self_opinion["Online"] == "ONLINE"
  end

  def self_opinion
    observations[name] ||= {}
  end

  def observations
    legacy_to_h["Observations"] ||= {}
  end

  def legacy_to_h
    @status.merge("Name" =>  name)
  end

  FALLBACK_SORTING = "😶😶😶"
  def sorting_key
    team = status.fetch("Flair", "").strip
    team = team == "" ? FALLBACK_SORTING : team[0]
    "#{team}#{name}"
  end

  def speculate(observation)
    @last_seen ||= observation["LastSeen"]
    observations[name] = observation.merge(self_opinion)
  end
end

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

      nara = (@db[name] ||= Nara.new(name))
      nara.status = status
      nara.mark_as_seen

      @db.values.each(&:mark_if_missing)

      speculate(@db.values)

      @db = @db.to_a.sort_by { |_, nara| nara.sorting_key }.to_h
    end
  end

  def speculate(narae)
    # backfill information for naras basd on neighbours in case there's something
    narae.each do |nara|
      nara.observations.each do |other_nara_name, observation|
        other_nara = (@db[other_nara_name] ||= Nara.new(other_nara_name))
        other_nara.speculate(observation)
      end
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

    # when naras boot they're observations are weak
    status["Observations"][name] ||= {}
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
  json({ naras: naraweb.db.values.map(&:legacy_to_h), server: NaraWeb.hostname })
end

get '/narae.json' do
  json({ naras: naraweb.db.values.map(&:to_h), server: NaraWeb.hostname })
end
