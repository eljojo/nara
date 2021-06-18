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
    @first_seen_internally = Time.now.to_i
  end

  def to_h
    last_restart = self_opinion.fetch("LastRestart", 0)
    last_restart = (last_seen&.to_i || @first_seen_internally) if last_restart == 0

    start_time = self_opinion.fetch("StartTime", 0)
    start_time = (last_seen&.to_i || @first_seen_internally) if start_time == 0
    {
      Name: name,
      Flair: status.fetch("Flair", ""),
      LicensePlate: status.fetch("LicensePlate", ""),
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
    self_opinion["Online"] = "ONLINE"
  end

  def mark_offline
    self_opinion["Online"] = "OFFLINE"
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
    team = status.fetch("LicensePlate", "").strip
    team = FALLBACK_SORTING if team == ""
    "#{team}#{name}"
  end

  def speculate(observation)
    @last_seen ||= observation["LastSeen"]
    if @status.fetch("LicensePlate", "").strip == "" && observation["ClusterEmoji"] != ""
      @status["LicensePlate"] = "🏳️ #{observation["ClusterEmoji"]}"
    end
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
      topic, name, status = @client.fetch
      next unless name

      nara = (@db[name] ||= Nara.new(name))

      case topic
      when /nara\/plaza/, /nara\/newspaper/
        nara.status = status
      end

      if topic =~ /chau/
        nara.mark_offline
      else
        nara.mark_as_seen
      end

      db_maintenance
    end
  end

  def db_maintenance
    @db.values.each(&:mark_if_missing)
    speculate(@db.values)
    @db = @db.to_a.sort_by { |_, nara| nara.sorting_key }.to_h
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

    case topic
    when /nara\/plaza/
      name = message["Name"]
      status = message.fetch("Status")
    when /nara\/newspaper/
      name = topic.split("/").last # TODO: replace for regex
      status = message
    when /nara\/ping/
      name = message["From"]
      status = message
    else
      raise "unknown topic #{topic}"
    end


    [topic, name, status]
  rescue MQTT::ProtocolException, SocketError, Errno::ECONNREFUSED
    disconnect
    sleep 1
    [nil, nil, nil]
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
    @client.subscribe('nara/plaza/#')
    @client.subscribe('nara/newspaper/#')
    @client.subscribe('nara/ping/#')
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
