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
  attr_accessor :api_url

  def initialize(name)
    @name = name
    @status = {}
    @last_seen = nil
    @first_seen_internally = Time.now.to_i
    @api_url = ""
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
    status["LicensePlate"] = ""
    status["Flair"] = ""
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
      self_opinion["Online"] = "MISSING"
      status["LicensePlate"] = ""
      status["Flair"] = ""
    end
  end

  def country_flag
    status.fetch("LicensePlate", "").split(" ").last
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

  def traefik_routers
    return {} if api_url.empty?
    routers = traefik_router("#{name}-api", "#{name}.nara.network")
    routers
  end

  def traefik_router(service_name, domain, router_name = "")
    {
      "#{service_name}#{router_name}" => {
        "entryPoints": [ "public" ],
        "middlewares": [  ],
        "rule": "Host(`#{domain}`)",
        "service": service_name
      },
      "#{service_name}#{router_name}-secure" => {
        "entryPoints": [ "public-secure" ],
        "middlewares": [ ],
        "rule": "Host(`#{domain}`)",
        "service": service_name,
        "tls": {}
      }
    }
  end

  def traefik_services
    return {} if api_url.empty?
    {
      "#{name}-api": {
        "loadBalancer": {
          "servers": [ { "url": api_url } ]
        }
      }
    }
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
      @status["LicensePlate"] = "#{observation["ClusterEmoji"]} 🏳️"
    end
    observations[name] = observation.merge(self_opinion)
  end
end

class NaraWeb
  def self.hostname
    @hostname ||= Socket.gethostname.split(".").first
  end

  def self.production?
    return @prod if defined?(@prod)
    @prod = (ENV['RACK_ENV'] == "production")
  end
  attr_reader :db

  def initialize(mqtt)
    @client = mqtt
    @db = {}
    @last_wave = {}
  end

  def start!
    loop do
      topic, name, status = @client.fetch
      next unless name

      nara = (@db[name] ||= Nara.new(name))

      case topic
      when /nara\/plaza\/chau/, /nara\/newspaper/
        nara.status = status
      when /nara\/selfie/
        nara.status = status.fetch("Status")
        api_url = status.fetch("ApiUrl", "")
        nara.api_url = api_url unless api_url.empty?
      when /nara\/wave/
        @last_wave = status
        next
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

  def traefik_db
    nara = @db.values.select(&:online?)
    {
      "http": {
        "routers": nara.map(&:traefik_routers).inject(&:merge),
        "services": nara.map(&:traefik_services).inject(&:merge)
      }
    }
  end

  def last_wave
    wave = @last_wave.dup
    wave['SeenBy'] = wave.fetch('SeenBy', []).map do |sb|
      name = sb['Nara']
      nara = (@db[name] ||= Nara.new(name))
      sb.merge('CountryFlag' => nara.country_flag)
    end
    wave
  end
end

class MqttClient
  def fetch
    topic, message_json = client.get
    message = JSON.parse(message_json)

    case topic
    when /nara\/plaza\/hey_there/
      name = message["Name"]
      status = message
    when /nara\/plaza\/chau/
      name = message["Name"]
      status = message.fetch("Status")
    when /nara\/selfie/
      name = topic.split("/").last # TODO: replace for regex
      status = message
    when /nara\/newspaper/
      name = topic.split("/").last # TODO: replace for regex
      status = message
    when /nara\/ping/
      name = message["From"]
      status = message
    when /nara\/wave/
      name = message["StartNara"]
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

  def connect
    client
  end

  private

  def client
    return @client if @client
    $log.info("connecting to MQTT server")
    @client = MQTT::Client.connect(MQTT_CONN)
    $log.info("connected to MQTT server")
    @client.subscribe('nara/plaza/#')
    @client.subscribe('nara/selfies/#')
    @client.subscribe('nara/newspaper/#')
    @client.subscribe('nara/ping/#')
    @client.subscribe('nara/wave')
    @client
  end
end

mqtt_host = ENV.fetch('MQTT_HOST', 'hass.eljojo.casa').split(':')[0]
MQTT_CONN = { username: ENV.fetch('MQTT_USER'), password: ENV.fetch('MQTT_PASS'), host: mqtt_host, ssl: true, client_id: "nara-web-#{NaraWeb.hostname}" }

$log = Logger.new(STDOUT)
$log.level = if NaraWeb.production? then Logger::INFO else Logger::DEBUG end
mqtt = MqttClient.new
mqtt.connect
naraweb = NaraWeb.new(mqtt)

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

get '/traefik.json' do
  json(naraweb.traefik_db)
end

get '/status/:name.json' do
  name = params['name']
  nara = naraweb.db[name]
  pass unless nara
  json(nara.legacy_to_h)
end

get '/last_wave.json' do
  json({ wave: naraweb.last_wave, server: NaraWeb.hostname })
end

# Add at the bottom of your file (after other routes):
get '/metrics' do
  content_type 'text/plain'
  lines = []

  # Optional: a "meta" metric to track Flair/LicensePlate as labels
  lines << "# HELP nara_info Basic string data from each Nara"
  lines << "# TYPE nara_info gauge"

  # Numeric metrics
  lines << "# HELP nara_online 1 if the Nara is ONLINE, else 0"
  lines << "# TYPE nara_online gauge"

  lines << "# HELP nara_buzz Buzz level reported by the Nara"
  lines << "# TYPE nara_buzz gauge"

  lines << "# HELP nara_chattiness Chattiness level reported by the Nara"
  lines << "# TYPE nara_chattiness gauge"

  lines << "# HELP nara_last_seen Unix timestamp when the Nara was last seen"
  lines << "# TYPE nara_last_seen gauge"

  lines << "# HELP nara_last_restart Unix timestamp when the Nara last restarted"
  lines << "# TYPE nara_last_restart gauge"

  lines << "# HELP nara_start_time Unix timestamp when the Nara started"
  lines << "# TYPE nara_start_time gauge"

  lines << "# HELP nara_uptime_seconds Uptime reported by the host"
  lines << "# TYPE nara_uptime_seconds gauge"

  lines << "# HELP nara_restarts_total Restart count from the Nara"
  lines << "# TYPE nara_restarts_total counter"

  # Loop through your Nara data as shown in /narae.json
  naraweb.db.values.each do |obj|
    data = obj.to_h
    name = data[:Name]

    # Add an info metric with strings as labels
    # caution: big or frequently changing strings can create label sprawl
    flair         = data[:Flair] || ""
    license_plate = data[:LicensePlate] || ""
    lines << "nara_info{name=\"#{name}\",flair=\"#{flair}\",license_plate=\"#{license_plate}\"} 1"

    # Convert "ONLINE" => 1, everything else => 0
    online_value = (data[:Online] == "ONLINE") ? 1 : 0
    lines << "nara_online{name=\"#{name}\"} #{online_value}"

    lines << "nara_buzz{name=\"#{name}\"} #{data[:Buzz]}"
    lines << "nara_chattiness{name=\"#{name}\"} #{data[:Chattiness]}"

    # Avoid writing nil; only emit metrics if we have real values
    lines << "nara_last_seen{name=\"#{name}\"} #{data[:LastSeen]}" if data[:LastSeen]
    lines << "nara_last_restart{name=\"#{name}\"} #{data[:LastRestart]}" if data[:LastRestart]
    lines << "nara_start_time{name=\"#{name}\"} #{data[:StartTime]}" if data[:StartTime]
    lines << "nara_uptime_seconds{name=\"#{name}\"} #{data[:Uptime]}" if data[:Uptime]

    # 'Restarts' is a running count, so we use a counter metric
    lines << "nara_restarts_total{name=\"#{name}\"} #{data[:Restarts]}"
  end

  lines.join("\n") + "\n"
end
