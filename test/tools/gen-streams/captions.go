package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zsiec/prism/test/tools/tsutil"
)

type captionLine struct {
	StartSec float64
	EndSec   float64
	Text     string
}

var captionScripts = map[string][]captionLine{
	"EN": {
		{0.5, 3.0, "Good evening, I'm reporting live from the studio."},
		{4.0, 7.0, "Tonight's top story: temperatures are rising across the region."},
		{8.5, 11.0, "We expect highs near 95 degrees by midweek."},
		{12.5, 15.0, "Emergency cooling centers have been opened in three counties."},
		{16.5, 19.0, "Officials urge residents to stay hydrated and check on neighbors."},
		{20.5, 23.5, "In other news, construction on the downtown bridge is ahead of schedule."},
		{25.0, 27.5, "The project is expected to finish two months early."},
		{29.0, 31.5, "Local businesses report increased foot traffic this quarter."},
		{33.0, 36.0, "The annual music festival will return to Riverside Park next month."},
		{37.5, 40.0, "Tickets are already sold out for Saturday's headliner."},
		{41.5, 44.0, "Sports: the home team clinched a playoff spot with tonight's win."},
		{45.5, 48.0, "Coach Martinez called it the best game of the season."},
		{49.5, 52.0, "Coming up after the break: your weekend weather forecast."},
		{53.5, 56.0, "Stay with us, we'll be right back."},
		{57.0, 59.5, "This has been your evening news update."},
	},
	"ES": {
		{1.0, 3.5, "Maria, no puedo creer lo que me estas diciendo."},
		{4.5, 7.0, "Es la verdad, Roberto. Lo vi con mis propios ojos."},
		{8.0, 10.5, "Pero eso es imposible. El nunca haria algo asi."},
		{11.5, 14.0, "Las personas cambian, Maria. Tu lo sabes mejor que nadie."},
		{15.5, 18.0, "Necesito hablar con el. Necesito escuchar su version."},
		{19.5, 22.0, "Ten cuidado. No todo es lo que parece en esta casa."},
		{23.0, 25.5, "Esta casa guarda muchos secretos, Roberto."},
		{27.0, 29.5, "Desde que llego la nueva directora, todo cambio."},
		{31.0, 33.5, "Ella tiene planes que nadie conoce todavia."},
		{35.0, 37.5, "Yo la escuche hablando por telefono anoche."},
		{39.0, 41.5, "Mencionaba algo sobre la herencia de Don Fernando."},
		{43.0, 45.5, "Eso es muy grave. Debemos actuar rapidamente."},
		{47.0, 49.5, "Manana por la noche, en el jardin. Alli hablaremos."},
		{51.0, 53.5, "De acuerdo. Pero nadie puede saber de esto."},
		{55.0, 58.0, "Confio en ti, Maria. Siempre lo he hecho."},
	},
	"FR": {
		{0.5, 3.0, "Bonsoir et bienvenue au journal de vingt heures."},
		{4.0, 7.0, "Ce soir, le president a annonce de nouvelles mesures economiques."},
		{8.0, 10.5, "Les marches financiers ont reagi positivement a cette annonce."},
		{12.0, 14.5, "Le taux de chomage continue de baisser ce trimestre."},
		{16.0, 18.5, "En Europe, les negociations commerciales avancent lentement."},
		{20.0, 22.5, "La delegation francaise reste optimiste malgre les difficultes."},
		{24.0, 26.5, "Maintenant, la meteo: un temps ensoleille est prevu demain."},
		{28.0, 30.5, "Les temperatures seront agreables dans la plupart des regions."},
		{32.0, 34.5, "Dans le monde du sport, victoire eclatante de l'equipe nationale."},
		{36.0, 38.5, "Le match s'est termine sur le score de trois a un."},
		{40.0, 42.5, "Passons maintenant a notre reportage special du soir."},
		{44.0, 46.5, "Un village provencal transforme par le tourisme durable."},
		{48.0, 50.5, "Les habitants partagent leur experience avec enthousiasme."},
		{52.0, 54.5, "Un modele qui pourrait inspirer d'autres communautes."},
		{56.0, 59.0, "C'etait le journal. Bonne soiree a tous."},
	},
	"DE": {
		{1.0, 3.5, "Die Welt der Ozeane birgt noch immer viele Geheimnisse."},
		{4.5, 7.0, "Tief unter der Oberflache leben erstaunliche Kreaturen."},
		{8.0, 10.5, "Forscher entdecken jedes Jahr neue Arten in der Tiefsee."},
		{12.0, 14.5, "Die Temperatur sinkt mit zunehmender Tiefe erheblich."},
		{16.0, 18.5, "In zweitausend Metern Tiefe herrscht ewige Dunkelheit."},
		{20.0, 22.5, "Doch selbst hier gedeiht das Leben in vielfaltiger Form."},
		{24.0, 26.5, "Biolumineszenz ist eine haeufige Anpassung dieser Tiere."},
		{28.0, 30.5, "Sie erzeugen ihr eigenes Licht um Beute anzulocken."},
		{32.0, 34.5, "Die Erforschung der Tiefsee erfordert modernste Technologie."},
		{36.0, 38.5, "Spezielle U-Boote koennen dem enormen Druck standhalten."},
		{40.0, 42.5, "Jede Expedition bringt ueberraschende Erkenntnisse."},
		{44.0, 46.5, "Der Meeresboden ist oft artenreicher als gedacht."},
		{48.0, 50.5, "Korallenriffe spielen eine entscheidende Rolle im Oekosystem."},
		{52.0, 54.5, "Ihr Schutz ist von globaler Bedeutung."},
		{56.0, 59.0, "Die Ozeane bleiben unser groesstes Abenteuer."},
	},
	"JA": {
		{1.0, 3.0, "Sore wa totemo kirei desu ne!"},
		{4.0, 6.5, "Kono machi wa hontou ni utsukushii desu."},
		{8.0, 10.5, "Watashi wa koko ni kuru no ga daisuki desu."},
		{12.0, 14.0, "Minna, chuumoku shite kudasai!"},
		{15.5, 18.0, "Taihen na koto ga okimashita."},
		{19.5, 22.0, "Demo shinpai shinaide kudasai. Daijoubu desu."},
		{23.5, 26.0, "Watashi ga mamotte agemasu."},
		{27.5, 30.0, "Issho ni ganbarou! Zettai ni akiramenai!"},
		{31.5, 34.0, "Kono tatakai wa mada owaranai."},
		{35.5, 38.0, "Yume wo akiramete wa ikemasen."},
		{39.5, 42.0, "Ashita wa kitto ii hi ni naru."},
		{43.5, 46.0, "Nakama ga iru kara daijoubu."},
		{47.5, 50.0, "Saigo made tatakau yo!"},
		{51.5, 54.0, "Arigato, minna. Hontou ni arigato."},
		{55.5, 58.5, "Mata ashita ne. Oyasumi nasai."},
	},
	"PT": {
		{0.5, 3.0, "E gol! Um gol espetacular do numero dez!"},
		{4.0, 6.5, "A torcida esta em delirio no estadio!"},
		{8.0, 10.5, "Que jogada maravilhosa. Assistencia perfeita."},
		{12.0, 14.5, "O time da casa domina completamente o jogo."},
		{16.0, 18.5, "Agora no segundo tempo, pressao total dos visitantes."},
		{20.0, 22.5, "O goleiro faz uma defesa incrivel!"},
		{24.0, 26.5, "Cartao amarelo para o jogador numero sete."},
		{28.0, 30.5, "Falta perigosa na entrada da area."},
		{32.0, 34.5, "O tecnico pede substituicao no meio-campo."},
		{36.0, 38.5, "Contra-ataque rapido pela direita."},
		{40.0, 42.5, "Cruzamento na area e cabecada do zagueiro!"},
		{44.0, 46.5, "Quase o segundo gol! A bola passou muito perto."},
		{48.0, 50.5, "Cinco minutos para o final do jogo."},
		{52.0, 54.5, "A tensao aumenta nas arquibancadas."},
		{56.0, 59.0, "E termina o jogo! Vitoria emocionante!"},
	},
	"KO": {
		{1.0, 3.5, "Oneul bam-ui teugbyeolhan iyagi-reul deul-eo boseyo."},
		{4.5, 7.0, "I dosi-neun arumdaun yeogsa-reul gajigo issseumnida."},
		{8.0, 10.5, "Oraedoen geonmul-deul-i yeojeonhi seo issseumnida."},
		{12.0, 14.5, "Saramdeul-eun jeontongeul sojunghi yeoggimnida."},
		{16.0, 18.5, "Geurigo saeroun byeonhwa-do hwan-yeong-hamnida."},
		{20.0, 22.5, "I gos-eun jeongmal teugbyeolhan jangso-imnida."},
		{24.0, 26.5, "Maeil sae iyagi-ga mandeul-eo jimnida."},
		{28.0, 30.5, "Saramdeul-eun seoro-reul doumyeo salgo issseumnida."},
		{32.0, 34.5, "Geu-geos-i i dosi-ui maeryeok-imnida."},
		{36.0, 38.5, "Bam-e-neun dosi-ga deo areumdawo jimnida."},
		{40.0, 42.5, "Bul-bich-i georireul balkge bi-chuimnida."},
		{44.0, 46.5, "Eumag sori-ga yeogi jeogi-eseo deullyeo omnida."},
		{48.0, 50.5, "Saramdeul-eun haengbog-hage georeogamnida."},
		{52.0, 54.5, "Oneul bam-do pyeonghwa-ropge jinaemnida."},
		{56.0, 59.0, "Gamsa-hamnida. Annyeonghi jumuseyo."},
	},
	"ZH": {
		{0.5, 3.0, "Ge wei guanzhong, wanshang hao."},
		{4.0, 6.5, "Jintian de zhuyao xinwen shi guanyu jingji fazhan."},
		{8.0, 10.5, "Guonei shengchan zongzhi zengzhang le baifenzhi wu."},
		{12.0, 14.5, "Zhe shi jinlai zui hao de jingji biaoxian."},
		{16.0, 18.5, "Guoji maoyi ye xianshi chu qiangjin de zengzhang shitou."},
		{20.0, 22.5, "Keji hangye jixu yinling chuangxin."},
		{24.0, 26.5, "Duo jia qiye xuanbu le xin de yanjiu xiangmu."},
		{28.0, 30.5, "Jiaoyu gaige ye zai wending tuijin zhong."},
		{32.0, 34.5, "Gengduo xuesheng xuanze le xin de zhuanye fangxiang."},
		{36.0, 38.5, "Tiyu xinwen: guojia dui zai bisai zhong biaoxian chuse."},
		{40.0, 42.5, "Yundong yuan men zhanshi le chuse de jingjishu."},
		{44.0, 46.5, "Tianqi yubao: mingtian qingtian, wendu shidu."},
		{48.0, 50.5, "Shiheyijiaren chumen huodong."},
		{52.0, 54.5, "Yishang shi jintian de xinwen huibao."},
		{56.0, 59.0, "Xiexie guankan, wanan."},
	},
	"AR": {
		{1.0, 3.5, "Ahlan wa sahlan. Nashrat al-akhbar al-masaaiya."},
		{4.5, 7.0, "Al-youm shahadna ahdathan muhimma fi al-mintaqa."},
		{8.0, 10.5, "Al-iqtisad yastamir fi al-numuw al-ijabi."},
		{12.0, 14.5, "Al-mufawadat al-siyasiya tatawasal bayn al-atraf."},
		{16.0, 18.5, "Fi al-riyadh, al-fariq al-watani haqaq fawzan kabiran."},
		{20.0, 22.5, "Al-jumhur ihtafal bi hadhihi al-nateeja al-ra'ia."},
		{24.0, 26.5, "Amma fi al-thaqafa, iftitah mahrajan jadeed."},
		{28.0, 30.5, "Fannanun min jami' anwa al-alam yusharikun."},
		{32.0, 34.5, "Al-turath al-thaqafi yuthlul markaz al-ihtimam."},
		{36.0, 38.5, "Al-taqs gadan: mashmus wa darajat harara mu'tadila."},
		{40.0, 42.5, "Al-riyah khafifa qaadima min al-shamal."},
		{44.0, 46.5, "Wa al-aan ma'a akhbar al-tiknulujiya."},
		{48.0, 50.5, "Sharikat jadida tu'lin an muntaj mubtakir."},
		{52.0, 54.5, "Hadhihi al-tiknulujiya qad tughayyir hayatana."},
		{56.0, 59.0, "Shukran li-mutaba'atikum. Tusbihun ala khayr."},
	},
	"IT": {
		{0.5, 3.0, "Benvenuti nella nostra cucina. Oggi prepariamo la pasta."},
		{4.0, 6.5, "Iniziamo con gli ingredienti freschi del mercato."},
		{8.0, 10.5, "L'olio extravergine di oliva e fondamentale."},
		{12.0, 14.5, "Aggiungiamo l'aglio tritato finemente nella padella."},
		{16.0, 18.5, "Il profumo che si diffonde e meraviglioso."},
		{20.0, 22.5, "Ora i pomodori San Marzano, i migliori per la salsa."},
		{24.0, 26.5, "Lasciamo cuocere a fuoco lento per dieci minuti."},
		{28.0, 30.5, "Nel frattempo, cuociamo la pasta al dente."},
		{32.0, 34.5, "Il segreto e nell'acqua di cottura ben salata."},
		{36.0, 38.5, "Uniamo la pasta alla salsa con un po' di acqua."},
		{40.0, 42.5, "Mantechiamo bene per creare una crema perfetta."},
		{44.0, 46.5, "Un pizzico di peperoncino per chi ama il piccante."},
		{48.0, 50.5, "Impiattamento: semplicita e eleganza italiana."},
		{52.0, 54.5, "Basilico fresco e parmigiano grattugiato sopra."},
		{56.0, 59.0, "Buon appetito a tutti! Alla prossima ricetta."},
	},
	"RU": {
		{1.0, 3.5, "Dobro pozhalovat v nash dokumentalny film."},
		{4.5, 7.0, "Segodnya my issleduyem severnye lesa Rossii."},
		{8.0, 10.5, "Eti lesa zanimayut milliony gektarov territorii."},
		{12.0, 14.5, "Priroda zdes sokhranila svoyu pervozdan'uyu krasotu."},
		{16.0, 18.5, "Zhivotnye obitayut v yestestvennoy srede."},
		{20.0, 22.5, "Medvedi gotovyatsya k zimnemu snu."},
		{24.0, 26.5, "Oni nakoplivayut zhir v techeniye vsego leta."},
		{28.0, 30.5, "Ptitsy pereletnye uzhe ulleteli na yug."},
		{32.0, 34.5, "Temperatura padayet nizhe nulya kazhdoy nochyu."},
		{36.0, 38.5, "Pervyy sneg pokryvayet derevya belym pokrovom."},
		{40.0, 42.5, "Eto volshebnoye vremya goda v sibiri."},
		{44.0, 46.5, "Mestnye zhiteli gotovyatsya k dlinnoy zime."},
		{48.0, 50.5, "Oni sobrayut drova i zapasayutsya produktami."},
		{52.0, 54.5, "Zhizn prodolzhayetsya dazhe v samyye kholodnye dni."},
		{56.0, 59.0, "Spasibo za prosmotr. Do novykh vstrech."},
	},
	"HI": {
		{0.5, 3.0, "Namaste, aaj ka natak shuru hota hai."},
		{4.0, 6.5, "Yeh kahani ek chote gaon se shuru hoti hai."},
		{8.0, 10.5, "Wahan ek ladki apne sapnon ke peechhe bhaagti hai."},
		{12.0, 14.5, "Uske pita kahte hain: beta, padhai pe dhyan do."},
		{16.0, 18.5, "Lekin uska dil gaana gaane mein lagta hai."},
		{20.0, 22.5, "Ek din use ek mauka milta hai sheher jaane ka."},
		{24.0, 26.5, "Woh bahut khush hoti hai aur taiyari karti hai."},
		{28.0, 30.5, "Maa kehti hain: apna khayal rakhna beta."},
		{32.0, 34.5, "Sheher mein sab kuch alag hai, naya hai."},
		{36.0, 38.5, "Lekin mushkilein bhi bahut aati hain raaste mein."},
		{40.0, 42.5, "Woh haar nahin maanti, himmat nahin harti."},
		{44.0, 46.5, "Aakhir mein uski mehnat rang laati hai."},
		{48.0, 50.5, "Woh apne sapne poore karti hai."},
		{52.0, 54.5, "Aur ghar wapas aake sabko gale lagati hai."},
		{56.0, 59.0, "Yeh kahani yahin khatam hoti hai. Dhanyavaad."},
	},
}

func injectCaptions(inputTS, output string, sc StreamConfig) error {
	if len(sc.Captions) == 0 {
		return tsutil.CopyFile(inputTS, output)
	}

	tmpDir, err := os.MkdirTemp("", "prism-captions-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	srtFiles := make([]string, 0, len(sc.Captions))
	for i, lang := range sc.Captions {
		srtPath := filepath.Join(tmpDir, fmt.Sprintf("caption_%d_%s.srt", i, lang))
		if err := writeSRT(srtPath, lang, sc.DurationSec); err != nil {
			return fmt.Errorf("write SRT for %s: %w", lang, err)
		}
		srtFiles = append(srtFiles, srtPath)
	}

	mode := sc.CaptionType
	if mode == "" {
		mode = "cea-608"
	}
	return embedCaptionsGoTool(inputTS, output, srtFiles, mode)
}

const scriptCycleSec = 60.0

// writeSRT generates an SRT file by looping the base script every 60 seconds
// until the full segment duration is covered.
func writeSRT(path, lang string, durationSec float64) error {
	lines, ok := captionScripts[lang]
	if !ok {
		lines = captionScripts["EN"]
	}

	var sb strings.Builder
	idx := 1
	for offset := 0.0; offset < durationSec; offset += scriptCycleSec {
		for _, line := range lines {
			start := offset + line.StartSec
			end := offset + line.EndSec
			if end > durationSec {
				break
			}

			startH := int(start) / 3600
			startM := (int(start) % 3600) / 60
			startS := int(start) % 60
			startMs := int((start - float64(int(start))) * 1000)

			endH := int(end) / 3600
			endM := (int(end) % 3600) / 60
			endS := int(end) % 60
			endMs := int((end - float64(int(end))) * 1000)

			sb.WriteString(fmt.Sprintf("%d\n", idx))
			sb.WriteString(fmt.Sprintf("%02d:%02d:%02d,%03d --> %02d:%02d:%02d,%03d\n",
				startH, startM, startS, startMs,
				endH, endM, endS, endMs))
			sb.WriteString(line.Text + "\n\n")
			idx++
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func embedCaptionsGoTool(inputTS, output string, srtFiles []string, mode string) error {
	rootDir := findProjectRoot()
	toolDir := filepath.Join(rootDir, "test", "tools", "inject-captions")

	args := []string{"run", toolDir, "--mode=" + mode, inputTS, output}
	args = append(args, srtFiles...)

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("inject-captions: %w", err)
	}
	return nil
}
