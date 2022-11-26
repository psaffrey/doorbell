package main

import (
	"encoding/json"
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/faiface/beep"
	"github.com/faiface/beep/flac"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
	"log"
	"os"
	"path/filepath"
	"time"
	"net/http"
	"bytes"
	"io/ioutil"
)

var SINGLE_SOUND_ENV_VAR = "DOORBELL_SINGLE_SOUND"
var DOUBLE_SOUND_ENV_VAR = "DOORBELL_DOUBLE_SOUND"

type player struct {
	streamer beep.StreamSeekCloser
	Path     string
}

// initialise a sound player
func (p *player) init() {
	var err error
	var format beep.Format

	f, err := os.Open(p.Path)
	if err != nil {
		log.Fatal(err)
	}

	extension := filepath.Ext(p.Path)

	if extension == ".wav" {
		p.streamer, format, err = wav.Decode(f)
	} else if extension == ".flac" {
		p.streamer, format, err = flac.Decode(f)
	} else if extension == ".mp3" {
		p.streamer, format, err = mp3.Decode(f)
	} else {
		log.Printf("unrecognised file extension %s\n", extension)
		os.Exit(1)
	}

	if err != nil {
		log.Fatal(err)
	}
	log.Printf("initialising stream for file %s\n", p.Path)
	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
}

// play a sound
func (p *player) play(done chan<- bool) {
	p.streamer.Seek(0)
	speaker.Play(beep.Seq(p.streamer, beep.Callback(func() {
		done <- true
	})))
}

type ButtonMessage struct {
	Action      string
	Battery     uint16
	Lastseen    uint64
	Linkquality uint16
}

// closure which creates a messages handler
// that will post a message on a Go channel when it receives an mqtt message
func make_listener(button chan<- mqtt.Message) mqtt.MessageHandler {
	return func(client mqtt.Client, msg mqtt.Message) {
		button <- msg
	}
}

// coordinate receiving messages and then playing the appropriate sound
func receiver(button <-chan mqtt.Message, finished chan<- bool, slack_url string) {
	playing := false
	single_path := os.Getenv(SINGLE_SOUND_ENV_VAR)
	double_path := os.Getenv(DOUBLE_SOUND_ENV_VAR)
	sp := player{Path: single_path}
	dp := player{Path: double_path}
	sp.init()
	dp.init()
	player_channel := make(chan bool)
	for {
		select {
		case msg, more := <-button:
			if more {
				log.Printf("received: %s\n", msg.Payload())
				var buttonmessage ButtonMessage
				e := json.Unmarshal(msg.Payload(), &buttonmessage)
				if e != nil {
					log.Println("problem unpacking message!")
					continue
				}
				if buttonmessage.Action == "" {
					log.Printf("ignoring empty message %s\n", buttonmessage.Action)
					continue
				}
				if playing {
					log.Println("Already playing")
					continue
				}
				if buttonmessage.Action == "single" {
					playing = true
					go sp.play(player_channel)
					if slack_url != "" {
						message := fmt.Sprintf("ding dong! (link quality %d; battery %d)", buttonmessage.Linkquality, buttonmessage.Battery)
						go slack_post(message, slack_url)
					}
				} else if buttonmessage.Action == "double" {
					playing = true
					go dp.play(player_channel)
					if slack_url != ""{
						message := fmt.Sprintf("ding dong! (link quality %d; battery %d)", buttonmessage.Linkquality, buttonmessage.Battery)
						go slack_post(message, slack_url)
					}
				}
			} else {
				log.Println("done")
				finished <- true
				return
			}
		case <-player_channel:
			log.Println("finished dinging")
			playing = false
		}
	}
	return
}

// call back functions to handle connecting to mqtt
var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	log.Println("Connected")
	sub(client)
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	log.Printf("Connect lost: %v\n", err)
}

// create the mqtt client we'll use to pick up messages
func setup_client(listener mqtt.MessageHandler) mqtt.Client {
	var broker = "192.168.0.100"
	var port = 1883
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", broker, port))
	clientid := fmt.Sprintf("go_mqtt_client-%s", hostname)
	log.Printf("using client ID: %s", clientid)
	opts.SetClientID(clientid)
	// opts.SetUsername("emqx")
	// opts.SetPassword("public")
	opts.SetDefaultPublishHandler(listener)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	return client
}

// post a message to a Slack channel using a webhook
func slack_post(message string, endpoint string) {
	postBody, _ := json.Marshal(map[string]string{
		"text": message,
	})
	messageBody := bytes.NewBuffer(postBody)
	resp, err := http.Post(endpoint, "application/json", messageBody)
	if err != nil {
		log.Fatalf("An Error Occured %v", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("message from Slack: %s", body)
}

// subscribe to the appropriate mqtt topic
func sub(client mqtt.Client) {
	topic := "sensors/Doorbell"
	token := client.Subscribe(topic, 1, nil)
	token.Wait()
	topic = "sensors/Button"
	token = client.Subscribe(topic, 1, nil)
	token.Wait()
	log.Printf("Subscribed to topic :%s\n", topic)
}

func main() {
	_, single_present := os.LookupEnv(SINGLE_SOUND_ENV_VAR)
	_, double_present := os.LookupEnv(DOUBLE_SOUND_ENV_VAR)
	if !single_present || !double_present {
		fmt.Printf("need to define %s and %s\n", SINGLE_SOUND_ENV_VAR, DOUBLE_SOUND_ENV_VAR)
		os.Exit(1)
	}

	slackPtr := flag.String("doslack", "", "webhook for Slack messages")
	flag.Parse()

	button := make(chan mqtt.Message)
	done := make(chan bool)

	listener := make_listener(button)

	client := setup_client(listener)

	go receiver(button, done, *slackPtr)

	defer client.Disconnect(250)
	select {}
}
