require 'mqtt'
require 'json'
require 'logger'
require 'sinatra'
require "sinatra/json"

class NaraWeb
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
      @db[name]["LastSeen"] = Time.now.to_i

      calculate_verdict!
      @db = @db.to_a.sort_by { |name, data| [data.dig("Verdict", "ClusterName"), name].compact }.to_h
    end
  end

  private

  def calculate_verdict!
    @db.each do |name, nara|
      obs = observations_for(name)
      nara["Verdict"] = sum_verdict(obs)
    end
  end

  def observations_for(name)
    res = []
    @db.each do |n_name, nara|
      next if name == n_name
      obs = nara["Observations"][name]
      res << obs if obs
    end
    res
  end

  def sum_verdict(obs)
    verdict = {}
    keys = obs.flat_map(&:keys).uniq
    keys.each do |key|
      values = obs.select{|o| o.key?(key) }.map { |o| o[key] }
      verdict[key] = values.max_by { |value| values.count(value) } 
    end
    verdict
  end
end

class MqttClient
  def fetch
    topic, message = client.get
    name = topic.split("/").last
    $log.debug("new update from #{name}")
    [name, JSON.parse(message)]
  rescue MQTT::ProtocolException, SocketError
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
    @client.subscribe(MQTT_TOPIC)
    @client
  end
end

MQTT_CONN = { username: ENV.fetch('MQTT_USER'), password: ENV.fetch('MQTT_PASS'), host: ENV.fetch('MQTT_HOST', 'hass.eljojo.casa'), ssl: true }
MQTT_TOPIC = 'nara/newspaper/#'

$log = Logger.new(STDOUT)
$log.level = if NaraWeb.production? then Logger::INFO else Logger::DEBUG end
naraweb = NaraWeb.new

Thread.new { naraweb.start! }

get '/' do
  erb :index
end

get '/api.json' do
  json naraweb.db
end
