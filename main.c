/*
 * ESP32 (ESP-IDF) -> MQTT -> Home Assistant (MQTT Discovery)
 *
 * 흐름:
 *   1. WiFi 연결
 *   2. MQTT 브로커 연결
 *   3. Discovery config topic 에 retain=true 로 1회 publish
 *      -> HA MQTT integration 이 자동으로 sensor entity 생성
 *   4. 주기적으로 state topic 에 온도값 publish
 *
 * 온도값은 실제로는 DHT22 / SHT31 / NTC ADC 등에서 읽어와야 합니다.
 * 아래 read_temperature() 안에 실제 센서 코드로 교체하세요.
 */

#include "cJSON.h"
#include "dht.h"
#include "driver/gpio.h"
#include "driver/ledc.h"
#include "esp_event.h"
#include "esp_log.h"
#include "esp_random.h"
#include "esp_system.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "mqtt_client.h"
#include "nvs_flash.h"
#include <stdio.h>
#include <string.h>
#include <time.h>

#define DHT_SENSOR_TYPE DHT_TYPE_AM2301 // DHT22/AM2302 는 DHT_TYPE_AM2301 사용
#define DHT_GPIO GPIO_NUM_4             // DATA 핀 연결된 GPIO 번호로 수정

#define LED_GPIO GPIO_NUM_2 // LED/릴레이 연결된 GPIO 번호로 수정
#define LED_ACTIVE_HIGH 1   // 릴레이가 active-low 모듈이면 0 으로 변경

#define LIGHT_PWM_GPIO GPIO_NUM_3 // PWM으로 조절할 새 조명 GPIO 번호

// LEDC (PWM) 설정
#define LEDC_TIMER LEDC_TIMER_0
#define LEDC_MODE LEDC_LOW_SPEED_MODE
#define LEDC_OUTPUT_IO LIGHT_PWM_GPIO
#define LEDC_CHANNEL LEDC_CHANNEL_0
#define LEDC_DUTY_RES LEDC_TIMER_13_BIT
#define LEDC_FREQUENCY (5000)

#define WIFI_SSID "TNT_HQ_2.4G"
#define WIFI_PASS "12345678901234"
#define MQTT_BROKER_URI "mqtt://192.168.0.118:1883" // Mosquitto 브로커 주소
#define MQTT_USERNAME "esp32_sensor"                // 없으면 NULL
#define MQTT_PASSWORD "pi"

// 디바이스 식별자 (HA 상에서 고유해야 함)
#define DEVICE_ID "esp32_sensor01"
#define DEVICE_NAME "ESP32 Sensor"

static const char *TAG = "ha_mqtt_temp";

static esp_mqtt_client_handle_t mqtt_client = NULL;

// topic 정의 (온도 센서)
static char discovery_topic[128];
static char state_topic[96];
static char availability_topic[96];

// my_assistant application topics (kept separate from Home Assistant topics)
static char app_discovery_topic[128];
static char app_telemetry_topic[128];
static char app_availability_topic[128];

// topic 정의 (LED/릴레이 스위치)
static char switch_discovery_topic[128];
static char switch_state_topic[96];
static char switch_command_topic[96];

// topic 정의 (LED 밝기 조절 number)
static char number_discovery_topic[128];
static char number_state_topic[96];
static char number_command_topic[96];

static bool led_state = false;
static int light_level = 0;

/* ---------- WiFi ---------- */

static void wifi_event_handler(void *arg, esp_event_base_t event_base,
                               int32_t event_id, void *event_data) {
  if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_START) {
    esp_wifi_connect();
  } else if (event_base == WIFI_EVENT &&
             event_id == WIFI_EVENT_STA_DISCONNECTED) {
    ESP_LOGW(TAG, "WiFi disconnected, retrying...");
    esp_wifi_connect();
  } else if (event_base == IP_EVENT && event_id == IP_EVENT_STA_GOT_IP) {
    ESP_LOGI(TAG, "WiFi connected, got IP");
  }
}

static void wifi_init(void) {
  ESP_ERROR_CHECK(esp_netif_init());
  ESP_ERROR_CHECK(esp_event_loop_create_default());
  esp_netif_create_default_wifi_sta();

  wifi_init_config_t cfg = WIFI_INIT_CONFIG_DEFAULT();
  ESP_ERROR_CHECK(esp_wifi_init(&cfg));

  ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID,
                                             &wifi_event_handler, NULL));
  ESP_ERROR_CHECK(esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP,
                                             &wifi_event_handler, NULL));

  wifi_config_t wifi_config = {
      .sta =
          {
              .ssid = WIFI_SSID,
              .password = WIFI_PASS,
          },
  };
  ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
  ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &wifi_config));
  ESP_ERROR_CHECK(esp_wifi_start());
}

/* ---------- LED/릴레이 PWM (LEDC) 제어 ---------- */

static void led_gpio_init(void) {
  gpio_config_t io_conf = {
      .pin_bit_mask = (1ULL << LED_GPIO),
      .mode = GPIO_MODE_OUTPUT,
      .pull_up_en = GPIO_PULLUP_DISABLE,
      .pull_down_en = GPIO_PULLDOWN_DISABLE,
      .intr_type = GPIO_INTR_DISABLE,
  };
  gpio_config(&io_conf);

  // 초기 상태: OFF
  gpio_set_level(LED_GPIO, LED_ACTIVE_HIGH ? 0 : 1);
}

static void led_pwm_init(void) {
  ledc_timer_config_t ledc_timer = {.speed_mode = LEDC_MODE,
                                    .duty_resolution = LEDC_DUTY_RES,
                                    .timer_num = LEDC_TIMER,
                                    .freq_hz = LEDC_FREQUENCY,
                                    .clk_cfg = LEDC_AUTO_CLK,
                                    .deconfigure = false};
  ESP_ERROR_CHECK(ledc_timer_config(&ledc_timer));

  ledc_channel_config_t ledc_channel = {.gpio_num = LEDC_OUTPUT_IO,
                                        .speed_mode = LEDC_MODE,
                                        .channel = LEDC_CHANNEL,
                                        .intr_type = LEDC_INTR_DISABLE,
                                        .timer_sel = LEDC_TIMER,
                                        .duty = 0,
                                        .hpoint = 0,
                                        .sleep_mode =
                                            LEDC_SLEEP_MODE_NO_ALIVE_NO_PD,
                                        .flags = {.output_invert = 0}};
  ESP_ERROR_CHECK(ledc_channel_config(&ledc_channel));
}

static void light_level_set_raw(int level) {
  light_level = level;
  uint32_t duty = (level * 8191) / 100;
  if (!LED_ACTIVE_HIGH) {
    duty = 8191 - duty;
  }
  ESP_ERROR_CHECK(ledc_set_duty(LEDC_MODE, LEDC_CHANNEL, duty));
  ESP_ERROR_CHECK(ledc_update_duty(LEDC_MODE, LEDC_CHANNEL));
  ESP_LOGI(TAG, "PWM level set to %d%% (duty %lu)", level, duty);
}

static void led_set(bool on) {
  led_state = on;
  int level;
  if (LED_ACTIVE_HIGH) {
    level = on ? 1 : 0;
  } else {
    level = on ? 0 : 1;
  }
  gpio_set_level(LED_GPIO, level);
  ESP_LOGI(TAG, "LED state set to %s on GPIO %d", on ? "ON" : "OFF", LED_GPIO);
}

static void light_level_set(int level) {
  if (level < 0)
    level = 0;
  if (level > 100)
    level = 100;

  light_level_set_raw(level);
}

/* ---------- 센서 읽기 (DHT22) ---------- */

// 성공 시 true 반환, 실패 시 false (읽기 실패는 흔함 - 재시도 로직 있음)
static bool read_temperature(float *out_temp_c) {
#if 1
  static float t = 24.0f;
  t += ((float)(esp_random() % 100) / 100.0f - 0.5f) * 0.3f;
  *out_temp_c = t;
  return true;
#else
  float humidity = 0.0f;
  float temperature = 0.0f;

  // DHT22 는 최소 2초 간격으로만 안정적으로 읽힘 (호출 주기 유의)
  esp_err_t err =
      dht_read_float_data(DHT_SENSOR_TYPE, DHT_GPIO, &humidity, &temperature);
  if (err != ESP_OK) {
    ESP_LOGW(TAG, "DHT22 read failed: %s", esp_err_to_name(err));
    return false;
  }

  *out_temp_c = temperature;
  ESP_LOGI(TAG, "DHT22 read OK: temp=%.1fC humidity=%.1f%%", temperature,
           humidity);
  return true;
#endif
}

/* ---------- MQTT Discovery ---------- */

static void publish_discovery_config(void) {
  // Home Assistant MQTT Discovery payload
  // https://www.home-assistant.io/integrations/mqtt/#mqtt-discovery
  cJSON *root = cJSON_CreateObject();
  cJSON_AddStringToObject(root, "name", "Temperature");
  cJSON_AddStringToObject(root, "unique_id", DEVICE_ID "_temp");
  cJSON_AddStringToObject(root, "state_topic", state_topic);
  cJSON_AddStringToObject(root, "unit_of_measurement", "°C");
  cJSON_AddStringToObject(root, "device_class", "temperature");
  cJSON_AddStringToObject(root, "value_template",
                          "{{ value_json.temperature }}");
  cJSON_AddStringToObject(root, "availability_topic", availability_topic);
  cJSON_AddStringToObject(root, "payload_available", "online");
  cJSON_AddStringToObject(root, "payload_not_available", "offline");

  cJSON *device = cJSON_CreateObject();
  cJSON *ids = cJSON_CreateArray();
  cJSON_AddItemToArray(ids, cJSON_CreateString(DEVICE_ID));
  cJSON_AddItemToObject(device, "identifiers", ids);
  cJSON_AddStringToObject(device, "name", DEVICE_NAME);
  cJSON_AddStringToObject(device, "manufacturer", "DIY");
  cJSON_AddStringToObject(device, "model", "ESP32");
  cJSON_AddItemToObject(root, "device", device);

  char *payload = cJSON_PrintUnformatted(root);

  // retain=true 로 보내야 HA/브로커 재시작 후에도 discovery 정보 유지됨
  esp_mqtt_client_publish(mqtt_client, discovery_topic, payload, 0, 1, true);
  ESP_LOGI(TAG, "Published discovery config: %s", discovery_topic);

  free(payload);
  cJSON_Delete(root);
}

static void publish_switch_discovery_config(void) {
  // https://www.home-assistant.io/integrations/switch.mqtt/
  cJSON *root = cJSON_CreateObject();
  cJSON_AddStringToObject(root, "name", "LED");
  cJSON_AddStringToObject(root, "unique_id", DEVICE_ID "_led");
  cJSON_AddStringToObject(root, "state_topic", switch_state_topic);
  cJSON_AddStringToObject(root, "command_topic", switch_command_topic);
  cJSON_AddStringToObject(root, "payload_on", "ON");
  cJSON_AddStringToObject(root, "payload_off", "OFF");
  cJSON_AddStringToObject(root, "state_on", "ON");
  cJSON_AddStringToObject(root, "state_off", "OFF");
  cJSON_AddStringToObject(root, "availability_topic", availability_topic);
  cJSON_AddStringToObject(root, "payload_available", "online");
  cJSON_AddStringToObject(root, "payload_not_available", "offline");

  // 온도 센서와 같은 device 로 묶어서 하나의 카드에 표시되게 함
  cJSON *device = cJSON_CreateObject();
  cJSON *ids = cJSON_CreateArray();
  cJSON_AddItemToArray(ids, cJSON_CreateString(DEVICE_ID));
  cJSON_AddItemToObject(device, "identifiers", ids);
  cJSON_AddStringToObject(device, "name", DEVICE_NAME);
  cJSON_AddStringToObject(device, "manufacturer", "DIY");
  cJSON_AddStringToObject(device, "model", "ESP32");
  cJSON_AddItemToObject(root, "device", device);

  char *payload = cJSON_PrintUnformatted(root);
  esp_mqtt_client_publish(mqtt_client, switch_discovery_topic, payload, 0, 1,
                          true);
  ESP_LOGI(TAG, "Published switch discovery config: %s",
           switch_discovery_topic);

  free(payload);
  cJSON_Delete(root);
}

static void publish_switch_state(void) {
  const char *payload = led_state ? "ON" : "OFF";
  // retain=true 로 보내야 HA 재시작/재접속 시 현재 상태를 바로 표시함
  esp_mqtt_client_publish(mqtt_client, switch_state_topic, payload, 0, 0, true);
  ESP_LOGI(TAG, "Published switch state: %s = %s", switch_state_topic, payload);
}

static void publish_number_discovery_config(void) {
  // https://www.home-assistant.io/integrations/number.mqtt/
  cJSON *root = cJSON_CreateObject();
  cJSON_AddStringToObject(root, "name", "Light Level");
  cJSON_AddStringToObject(root, "unique_id", DEVICE_ID "_light_level");
  cJSON_AddStringToObject(root, "state_topic", number_state_topic);
  cJSON_AddStringToObject(root, "command_topic", number_command_topic);
  cJSON_AddNumberToObject(root, "min", 0);
  cJSON_AddNumberToObject(root, "max", 100);
  cJSON_AddNumberToObject(root, "step", 1);
  cJSON_AddStringToObject(root, "availability_topic", availability_topic);
  cJSON_AddStringToObject(root, "payload_available", "online");
  cJSON_AddStringToObject(root, "payload_not_available", "offline");

  cJSON *device = cJSON_CreateObject();
  cJSON *ids = cJSON_CreateArray();
  cJSON_AddItemToArray(ids, cJSON_CreateString(DEVICE_ID));
  cJSON_AddItemToObject(device, "identifiers", ids);
  cJSON_AddStringToObject(device, "name", DEVICE_NAME);
  cJSON_AddStringToObject(device, "manufacturer", "DIY");
  cJSON_AddStringToObject(device, "model", "ESP32");
  cJSON_AddItemToObject(root, "device", device);

  char *payload = cJSON_PrintUnformatted(root);
  esp_mqtt_client_publish(mqtt_client, number_discovery_topic, payload, 0, 1,
                          true);
  ESP_LOGI(TAG, "Published number discovery config: %s",
           number_discovery_topic);

  free(payload);
  cJSON_Delete(root);
}

static void publish_light_level_state(void) {
  char payload[16];
  snprintf(payload, sizeof(payload), "%d", light_level);
  esp_mqtt_client_publish(mqtt_client, number_state_topic, payload, 0, 0, true);
  ESP_LOGI(TAG, "Published light level state: %s = %s", number_state_topic,
           payload);
}

static void publish_temperature(float temp_c) {
  cJSON *root = cJSON_CreateObject();
  cJSON_AddNumberToObject(root, "temperature", temp_c);
  char *payload = cJSON_PrintUnformatted(root);

  esp_mqtt_client_publish(mqtt_client, state_topic, payload, 0, 0, false);
  ESP_LOGI(TAG, "Published state: %s = %s", state_topic, payload);

  free(payload);
  cJSON_Delete(root);
}

static void publish_app_discovery_config(void) {
  cJSON *root = cJSON_CreateObject();
  cJSON_AddNumberToObject(root, "schema", 1);
  cJSON_AddStringToObject(root, "device_id", DEVICE_ID);
  cJSON_AddStringToObject(root, "name", DEVICE_NAME);
  cJSON_AddStringToObject(root, "device_type", "sensor");
  cJSON_AddStringToObject(root, "transport", "mqtt");
  char *payload = cJSON_PrintUnformatted(root);
  esp_mqtt_client_publish(mqtt_client, app_discovery_topic, payload, 0, 1,
                          true);
  ESP_LOGI(TAG, "Published my_assistant discovery: %s", app_discovery_topic);
  free(payload);
  cJSON_Delete(root);
}

static void publish_app_availability(const char *state) {
  esp_mqtt_client_publish(mqtt_client, app_availability_topic, state, 0, 1,
                          true);
  ESP_LOGI(TAG, "Published my_assistant availability: %s = %s",
           app_availability_topic, state);
}

static void publish_app_telemetry(float temp_c) {
  time_t now;
  struct tm utc;
  char timestamp[32];
  time(&now);
  gmtime_r(&now, &utc);
  strftime(timestamp, sizeof(timestamp), "%Y-%m-%dT%H:%M:%SZ", &utc);

  cJSON *root = cJSON_CreateObject();
  cJSON_AddNumberToObject(root, "schema", 1);
  cJSON_AddStringToObject(root, "device_id", DEVICE_ID);
  cJSON_AddStringToObject(root, "timestamp", timestamp);
  cJSON *measurements = cJSON_CreateObject();
  cJSON_AddNumberToObject(measurements, "temperature_c", temp_c);
  cJSON_AddItemToObject(root, "measurements", measurements);
  char *payload = cJSON_PrintUnformatted(root);
  esp_mqtt_client_publish(mqtt_client, app_telemetry_topic, payload, 0, 0,
                          false);
  ESP_LOGI(TAG, "Published my_assistant telemetry: %s = %s",
           app_telemetry_topic, payload);
  free(payload);
  cJSON_Delete(root);
}

/* ---------- MQTT 이벤트 ---------- */

static void mqtt_event_handler(void *handler_args, esp_event_base_t base,
                               int32_t event_id, void *event_data) {
  esp_mqtt_event_handle_t event = (esp_mqtt_event_handle_t)event_data;

  switch ((esp_mqtt_event_id_t)event_id) {
  case MQTT_EVENT_CONNECTED:
    ESP_LOGI(TAG, "MQTT connected");
    esp_mqtt_client_publish(mqtt_client, availability_topic, "online", 0, 1,
                            true);
    publish_app_availability("online");
    publish_discovery_config();
    publish_app_discovery_config();
    publish_switch_discovery_config();
    publish_switch_state();
    publish_number_discovery_config();
    publish_light_level_state();

    esp_mqtt_client_subscribe(mqtt_client, switch_command_topic, 1);
    ESP_LOGI(TAG, "Subscribed to: %s", switch_command_topic);
    esp_mqtt_client_subscribe(mqtt_client, number_command_topic, 1);
    ESP_LOGI(TAG, "Subscribed to: %s", number_command_topic);
    break;
  case MQTT_EVENT_DISCONNECTED:
    ESP_LOGW(TAG, "MQTT disconnected");
    break;
  case MQTT_EVENT_DATA:
    if (strncmp(event->topic, switch_command_topic, event->topic_len) == 0 &&
        strlen(switch_command_topic) == event->topic_len) {
      char payload[16] = {0};
      int len = event->data_len < (int)sizeof(payload) - 1
                    ? event->data_len
                    : (int)sizeof(payload) - 1;
      memcpy(payload, event->data, len);

      ESP_LOGI(TAG, "Command received on %s: %s", switch_command_topic,
               payload);

      if (strcmp(payload, "ON") == 0) {
        led_set(true);
      } else if (strcmp(payload, "OFF") == 0) {
        led_set(false);
      } else {
        ESP_LOGW(TAG, "Unknown payload: %s", payload);
        break;
      }

      publish_switch_state();
    } else if (strncmp(event->topic, number_command_topic, event->topic_len) ==
                   0 &&
               strlen(number_command_topic) == event->topic_len) {
      char payload[16] = {0};
      int len = event->data_len < (int)sizeof(payload) - 1
                    ? event->data_len
                    : (int)sizeof(payload) - 1;
      memcpy(payload, event->data, len);

      ESP_LOGI(TAG, "Level command received on %s: %s", number_command_topic,
               payload);

      int level = atoi(payload);
      light_level_set(level);

      publish_light_level_state();
    }
    break;
  case MQTT_EVENT_ERROR:
    ESP_LOGE(TAG, "MQTT error");
    break;
  default:
    break;
  }
}

static void mqtt_app_start(void) {
  snprintf(discovery_topic, sizeof(discovery_topic),
           "homeassistant/sensor/%s/temperature/config", DEVICE_ID);
  snprintf(state_topic, sizeof(state_topic), "esp32/%s/state", DEVICE_ID);
  snprintf(availability_topic, sizeof(availability_topic),
           "esp32/%s/availability", DEVICE_ID);

  snprintf(app_discovery_topic, sizeof(app_discovery_topic),
           "my_assistant/v1/%s/discovery", DEVICE_ID);
  snprintf(app_telemetry_topic, sizeof(app_telemetry_topic),
           "my_assistant/v1/%s/telemetry", DEVICE_ID);
  snprintf(app_availability_topic, sizeof(app_availability_topic),
           "my_assistant/v1/%s/availability", DEVICE_ID);

  snprintf(switch_discovery_topic, sizeof(switch_discovery_topic),
           "homeassistant/switch/%s/led/config", DEVICE_ID);
  snprintf(switch_state_topic, sizeof(switch_state_topic), "esp32/%s/led/state",
           DEVICE_ID);
  snprintf(switch_command_topic, sizeof(switch_command_topic),
           "esp32/%s/led/set", DEVICE_ID);

  snprintf(number_discovery_topic, sizeof(number_discovery_topic),
           "homeassistant/number/%s/light_level/config", DEVICE_ID);
  snprintf(number_state_topic, sizeof(number_state_topic),
           "esp32/%s/light_level/state", DEVICE_ID);
  snprintf(number_command_topic, sizeof(number_command_topic),
           "esp32/%s/light_level/set", DEVICE_ID);

  esp_mqtt_client_config_t mqtt_cfg = {
      .broker.address.uri = MQTT_BROKER_URI,
      .credentials.username = MQTT_USERNAME,
      .credentials.authentication.password = MQTT_PASSWORD,
      // LWT: 비정상 종료 시 자동으로 offline 알림
      .session.last_will.topic = availability_topic,
      .session.last_will.msg = "offline",
      .session.last_will.qos = 1,
      .session.last_will.retain = true,
  };

  mqtt_client = esp_mqtt_client_init(&mqtt_cfg);
  esp_mqtt_client_register_event(mqtt_client, ESP_EVENT_ANY_ID,
                                 mqtt_event_handler, NULL);
  esp_mqtt_client_start(mqtt_client);
}

/* ---------- 주기적 온도 publish 태스크 ---------- */

static void temperature_task(void *pvParameters) {
  while (1) {
    if (mqtt_client != NULL) {
      float t;
      if (read_temperature(&t)) {
        publish_temperature(t);
        publish_app_telemetry(t);
      }
      // 실패 시 이번 주기는 건너뛰고 다음 주기에 재시도
    }
    vTaskDelay(pdMS_TO_TICKS(
        30 * 1000)); // 30초마다 publish (DHT22 최소 간격 2초 이상 준수)
  }
}

/* ---------- app_main ---------- */

void app_main(void) {
  ESP_ERROR_CHECK(nvs_flash_init());
  led_gpio_init();
  led_pwm_init();
  wifi_init();

  // WiFi 연결될 시간 확보 (간단히 대기; 프로덕션에서는 event group 권장)
  vTaskDelay(pdMS_TO_TICKS(5000));

  mqtt_app_start();

  xTaskCreate(temperature_task, "temperature_task", 4096, NULL, 5, NULL);
}
