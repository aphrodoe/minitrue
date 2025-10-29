const int sensorPin = A0;
const int deviceID = 1;

void setup() {
  Serial.begin(9600);
}

void loop() {
  int reading = analogRead(sensorPin);
  float voltage = reading * (5.0 / 1023.0);
  float temperatureC = voltage * 100;
  Serial.print("sensor_");
  Serial.print(deviceID);
  Serial.print(",");
  Serial.println(temperatureC);
  delay(1000);
}
