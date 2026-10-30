from PIL import Image,ImageDraw
from pathlib import Path
import math
p=Path(__file__).parent/'assets/base.png';img=Image.open(Path(__file__).parent/'assets/source_base.png').convert('RGB')
orig=img.copy();w,h=img.size
# Rebuild former black top mask entirely as a city glow; dark fill only in the two future card interiors.
for y in range(0,109):
    for x in range(96,384):
        ed=min(x-96,383-x)
        a=min(1.,max(0.,ed/12.))
        a*=min(1.,max(0.,(109-y)/12.))
        l=0.5+0.5*math.sin(x*.062+y*.032)
        sky=(int(10+9*y/109+5*l),int(5+3*l),int(30+22*y/109+13*l))
        z=orig.getpixel((x,y))
        img.putpixel((x,y),tuple(round(a*sky[i]+(1-a)*z[i]) for i in range(3)))
d=ImageDraw.Draw(img,'RGB')
# A very subtle city-glow layer, visible only between the cards.
for k in range(22):
    x=103+12*k
    height=6+(k*17)%18
    for y in range(104-height,105):
        if 0<=y<109:
            pix=img.getpixel((x,y))
            d.line((x,y,x+7,y),fill=tuple(round(v*.75) for v in pix))
    if k%2==0:
        d.point((x+3,101-height),fill=(93,29,128))
# Unified RAM card: cover the old short RAM and WiFi/NET frame with one full card.
d.rounded_rectangle((314,109,463,218),radius=7,fill=(14,7,34),outline=(242,71,236),width=2)
d.rounded_rectangle((318,113,459,214),radius=5,outline=(113,47,157),width=1)
# Distinct top frames filled locally; no additional solid fill in the top outside these rectangles.
for x0,y0,x1,y1,c in [(103,8,377,49,(242,74,243)),(103,55,377,96,(44,223,255))]:
    d.rounded_rectangle((x0-1,y0-1,x1+1,y1+1),radius=7,outline=tuple(min(255,i//2+20) for i in c),width=2)
    d.rounded_rectangle((x0,y0,x1,y1),radius=6,fill=(10,5,31),outline=c,width=2)
    d.line((x0+7,y0+2,x0+30,y0+2),fill=(250,235,255),width=1)
    d.line((x1-29,y1-2,x1-7,y1-2),fill=c,width=1)
# darken the data part only; no rectangular backdrop behind the frames
# Replace the baked-in DISK D: heading with DISK Z: (this PC uses C: and Z:).
# Use our shipped Z glyph bitmap; only a 7x9 rectangle of the base is changed.
import json
atlas=Image.open(Path(__file__).parent/'assets/fonts/10.png').convert('RGBA')
g=json.loads((Path(__file__).parent/'assets/fonts/10.json').read_text())['glyphs']['Z']
for yy in range(221,231):
    for xx in range(228,235):
        img.putpixel((xx,yy),img.getpixel((240,yy)))
for yy in range(16):
    for xx in range(g['w']):
        a=atlas.getpixel((g['x']+xx,yy))[0]/255.0
        if not a: continue
        px,py=228+xx,219+yy
        if not (0 <= px < w and 221 <= py < 231): continue
        bg=img.getpixel((px,py))
        fg=(177,164,241)
        img.putpixel((px,py), tuple(round(bg[i]*(1-a)+fg[i]*a) for i in range(3)))
img.save(p)
print(p,img.size)
