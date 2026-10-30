from PIL import Image,ImageFont,ImageDraw
from pathlib import Path
import json
out=Path(__file__).parent/'assets/fonts'
out.mkdir(parents=True,exist_ok=True)
fontpath='/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf'
chars=' 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZАБВГДЕЁЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯ.%$:/+-|°?'
for size in [10,12,14,17,19,22,26,28,34,40]:
    f=ImageFont.truetype(fontpath,size)
    boxes=[f.getbbox(ch) or (0,0,1,1) for ch in chars]
    yheight=max(b[3] for b in boxes)+4
    width=sum(max(1,b[2]-b[0])+3 for b in boxes)+2
    im=Image.new('L',(width,yheight),0);d=ImageDraw.Draw(im)
    x=1;glyphs={}
    for ch,b in zip(chars,boxes):
        gw=max(1,b[2]-b[0]);
        if ch!=' ':d.text((x-b[0],2),ch,font=f,fill=255)
        glyphs[ch]={'x':x,'w':gw}
        x+=gw+3
    im.save(out/f'{size}.png')
    (out/f'{size}.json').write_text(json.dumps({'size':size,'height':yheight,'glyphs':glyphs},ensure_ascii=False),encoding='utf8')
print('Generated',len(list(out.glob('*.png'))),'atlases')
