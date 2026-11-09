VISCA-Bridge 1.0
================

Wozu?
-----
Der Tenveo-Controller schickt im Netzwerkmodus immer die VISCA-Adresse 1.
Die Bridge nimmt Befehle auf einem eigenen Port pro Kamera an, setzt die
richtige Adresse (81 -> 82 fuer Kamera 2) und schickt alles ueber eine
Verbindung an den Waveshare-Konverter. Antworten der Kameras gehen an den
richtigen Absender zurueck.

  Controller / Companion  --TCP-->  Bridge :5001 -> Adresse 1  --+
                                    Bridge :5002 -> Adresse 2  --+--> Waveshare :4196 --RS485--> Kameras


Dateien
-------
  visca-bridge.exe               Version mit Konsolenfenster (zum Testen)
  visca-bridge-ohne-fenster.exe  Gleiche Version ohne Fenster (Dauerbetrieb)
  config.json                    Einstellungen
  Autostart-einrichten.bat       Startet die Version ohne Fenster bei jeder Anmeldung
  Autostart-entfernen.bat        Nimmt sie wieder aus dem Autostart
  Beenden.bat                    Beendet die Bridge
  visca-bridge.log               Protokoll (entsteht beim ersten Start)


Einrichtung
-----------
1. Ordner an einen festen Platz kopieren, z. B. C:\VISCA-Bridge
2. config.json pruefen (mit Editor oeffnen):
     converter_host  = IP des Waveshare (192.168.133.200)
     converter_port  = 4196
     cameras         = Port und VISCA-Adresse pro Kamera
3. visca-bridge.exe einmal per Doppelklick starten.
   Windows fragt nach der Firewall: "Private Netzwerke" erlauben.
   Im Fenster muss "Verbunden mit Konverter" stehen.
4. Kameras an die Adressen anpassen: Kamera 1 = VISCA-Adresse 1,
   Kamera 2 = VISCA-Adresse 2 (wie bisher im seriellen Betrieb).
5. Tenveo-Controller (Netzwerkmodus), pro Kamera ein Netzwerkgeraet:
     Kamera 1: Protokoll VISCA(TCP), IP = dieser PC, Port 5001
     Kamera 2: Protokoll VISCA(TCP), IP = dieser PC, Port 5002
6. Companion, zwei Verbindungen "PTZOptics VISCA":
     IP = dieser PC (oder 127.0.0.1, wenn Companion auf demselben PC laeuft)
     Port 5001 bzw. 5002
7. Klappt alles: Konsolenfenster schliessen und
   Autostart-einrichten.bat ausfuehren.

Wichtig: Der PC braucht eine feste IP (in der Fritzbox "Diesem Netzwerkgeraet
immer die gleiche IPv4-Adresse zuweisen"), sonst findet der Controller die
Bridge nach einem Neustart nicht mehr.


Status und Fehlersuche
----------------------
Statusseite im Browser:  http://localhost:8099
  zeigt, ob der Konverter verbunden ist und wer an welchem Port haengt.

Protokoll: visca-bridge.log im selben Ordner.
Fuer Details "debug": true in config.json setzen und neu starten,
dann steht jeder Befehl und jede Antwort als Hex im Protokoll.

Weitere Kamera: in config.json einen Eintrag ergaenzen, z. B.
  { "name": "Kamera 3", "listen_port": 5003, "visca_address": 3 }

"visca_address": 0 = Adresse nicht veraendern (reines Durchreichen).
