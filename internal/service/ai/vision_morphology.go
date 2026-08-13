package ai

// vision_morphology.go — SINGLE SOURCE OF TRUTH for photo morphology.
//
// Edit THIS file when you change how we name / group what we see on photos.
// Admin skin-review, onboarding vision, and daily check-in vision all concatenate
// VisionMorphologyRules() into their system prompts. Text coaches that do not
// see the photo use VisionMorphologyCoachGuard() so they do not relabel and
// then give the wrong care advice.
//
// Schema-specific JSON mapping stays in each pipeline (admin enums, onboarding
// concern_types, check-in bullets) — do not fork the visual rules there.

// VisionMorphologyRules is the shared visual taxonomy + care-direction lock.
// Schema-agnostic: names groups in plain Vietnamese; each pipeline maps groups
// onto its own JSON after this block.
func VisionMorphologyRules() string {
	return `## Hình thái da (NGUỒN CHUNG — BẮT BUỘC, mọi pipeline ảnh)
Quan sát ĐÚNG hình thái rồi mới khuyên dưỡng/trị. Sai tên nhóm → routine / tips / sản phẩm sai hết.
Chỉ mô tả những gì nhìn thấy. Không bịa. Không bệnh danh y khoa cứng (eczema/rosacea/herpes chắc…).

## Kết luận tự tin trên ảnh rõ (BẮT BUỘC)
Khi ảnh đủ sáng và dấu hiệu rõ → **NÓI THẲNG**, ít hedge.
- Ưu tiên: “Đây là…”, “Má của mày đang…”, “Má của mày đang có mụn ẩn…”, “Má của mày đang có nhiều nốt nhỏ màu da nổi cao, trông giống mụn ẩn hoặc milia.”, “Má của mày đang sần sùi rõ, nhiều nốt nhỏ nổi cao + bề mặt da gồ ghề không đều.”, “Trông đúng kiểu…”, “Có nốt đầu trắng.”
- **CẤM nhồi** khi ảnh rõ: “không chắc 100% chỉ từ một ảnh”, “chưa chắc”, “trên ảnh nghi…”, “đôi khi liên quan…”, “có thể là…”, “trông giống…” kiểu né kết luận.
- **NGOẠI LỆ BẮT BUỘC (không phải hedge):** má nốt màu da tròn/mịn, không đỏ/mủ → **“trông giống mụn ẩn hoặc milia”**. Má sần/gồ không đều, không đỏ → **“sần sùi rõ… gồ ghề không đều”** (không hedge). **CẤM** “trông giống mụn thịt” trên má.
- Chỉ hedge khi ảnh **thật sự mờ / phản sáng mạnh / crop quá kém** không đọc được dấu — 1 câu ngắn.

## Phân loại nhóm khi đủ dấu hiệu ảnh (BẮT BUỘC — prose đời thường)
Gọi tên nhóm theo hình thái nhìn thấy (không phải chẩn đoán bệnh viện):
- đỏ + sưng nổi → **mụn viêm**
- có đầu trắng / vàng rõ → **mụn có mủ** (chỉ khi nốt trên da mặt kiểu má/trán/cằm **xa** viền môi — xem rule mép miệng)
- ổ to, căng, sâu → **mụn bọc / viêm sâu**
- nhiều nốt nhỏ **li ti dưới da**, bề mặt **gồ ghề nhẹ**, **không mủ / không vỡ**, **ít/không đỏ** → **mụn ẩn** thuần
- nhiều nốt nhỏ **màu da nổi cao trên má**, **tròn / mịn**, riêng lẻ hoặc cụm nhỏ, **không gồ ghề nhiều**, **không đỏ sưng**, **không mủ** → **trông giống mụn ẩn hoặc milia** — **CẤM** gọi Nốt đỏ sưng; **CẤM** mụn thịt
- má **sần sùi rõ**, nhiều nốt nhỏ nổi + bề mặt **gồ ghề không đều** (có thể kèm thâm/lõm nông), **không đỏ sưng**, **không mủ** → **sần sùi / texture không đều** — **CẤM** Nốt đỏ sưng / mụn viêm
- nhiều nốt nhỏ li ti **+ đỏ hồng rõ** → **mụn ẩn kèm kích ứng/viêm nhẹ** — **CẤM** “chỉ mụn ẩn” / “không viêm”
- lỗ đen nhỏ hở miệng, ít đỏ → **mụn cồi** / đầu đen — **không** gọi mụn ẩn
- màu **nâu / xám phẳng** → **thâm / sắc tố** — **không** gọi mụn ẩn
- chùm hạt đỏ sưng **sát / trên viền môi** + tín hiệu viêm cấp → **viêm cấp sát mép miệng** (không gọi “mụn có mủ”)
- màu nâu/xám sẫm quanh khóe miệng–cằm, bề mặt phẳng / ít nổi, **không** chùm hạt đỏ sưng → **thâm / sắc tố quanh miệng** — **CẤM** gọi “viêm cấp sát mép”
- nhiều nốt nhỏ **màu da / nâu nhạt**, nổi cao, **không đỏ sưng**, vùng **cổ / nách / thân / mí mắt** (nốt mềm, có cuống hoặc dẹt) → **trông giống mụn thịt** (skin tag) — **CẤM** áp cho cụm dày trên má
- ảnh cổ, **nếp ngang / nếp gấp** rõ, **không** cụm nốt đỏ sưng, **không** nốt màu da nổi cao kiểu mụn thịt → **nếp gấp / nếp ngang cổ**

## Mụn ẩn / closed comedones (BẮT BUỘC — tách khỏi thâm / viêm / mụn cồi)
Khi ảnh hoặc user_question khớp nốt nhỏ dưới da trên mặt (má/trán/cằm…):

### A — Mụn ẩn thuần (ít/không đỏ)
- Nhiều nốt nhỏ **li ti màu da / trắng**, bề mặt **gồ ghề nhẹ**, **không mủ / không vỡ**, **ít hoặc không đỏ hồng**
→ Prose **“mụn ẩn”**. **CẤM** gọi Nốt đỏ sưng / mụn viêm khi ít/không đỏ.
→ **CẤM phủ nhận** mụn ẩn khi dấu hiệu rõ.

### B — Nốt nhỏ + đỏ hồng rõ / bóng (viêm hoặc kích ứng kèm)
- Nhiều nốt nhỏ li ti **và** má/vùng da **đỏ hồng khá nhiều** (và/hoặc bóng dầu rõ)
→ Prose **BẮT BUỘC**: vừa **mụn ẩn** vừa **kích ứng / viêm nhẹ**. Câu đầu ưu tiên đỏ + nốt nhỏ, không chỉ “mụn ẩn dày đặc”.
→ **CẤM**: “không thấy dấu hiệu viêm cấp”, “không viêm”, “chỉ mụn ẩn suông”, “mụn ẩn dày đặc” mà bỏ đỏ khi ảnh đỏ khá nhiều.
→ Hướng xử lý: làm sạch dịu + đủ ẩm; **tránh** acid/retinol/chà mạnh; đỏ không giảm → khám da liễu — **CẤM** đẩy BHA/retinol ngay.

### Phân biệt cứng (mọi nhánh)
  · **Mụn ẩn thuần** = nốt nhỏ dưới da, ít/không đỏ
  · **Má nốt màu da nổi cao, tròn/mịn, ít gồ** (không đỏ, không mủ) = **trông giống mụn ẩn hoặc milia** — **CẤM** mụn thịt / Nốt đỏ sưng
  · **Má sần sùi / gồ ghề không đều** (nhiều nốt nổi + bề mặt không đều, không đỏ) = **sần sùi / texture không đều** — **CẤM** “Nốt đỏ sưng”
  · **Thâm nâu** = sắc tố phẳng
  · **Mụn viêm** = nốt đỏ sưng nổi rõ từng hạt
  · **Mụn cồi / đầu đen** = lỗ đen hở miệng
→ **CẤM** nhầm mụn ẩn/milia trên má ↔ mụn thịt cổ–nách–mí mắt.
→ User hỏi “có phải kích ứng không” + ảnh đỏ rõ → trả lời thẳng có kích ứng/viêm nhẹ kèm nốt — **CẤM** phủ nhận.

## Má — nốt nhỏ màu da / sần sùi (BẮT BUỘC — tách 2 case, CẤM mụn thịt, CẤM “Nốt đỏ sưng” khi không đỏ)
Khi ảnh **má** (trán/cằm mặt cũng vậy) có nốt nhỏ **không đỏ sưng / không mủ**, chọn **đúng 1 nhánh**. Nhánh B thắng nếu bề mặt sần/gồ rõ.

### A — Nốt tròn, mịn, riêng lẻ / cụm nhỏ (mụn ẩn hoặc milia)
Dấu hiệu: nốt nhỏ **màu da**, **tròn**, **mịn**, nổi cao; riêng lẻ hoặc cụm nhỏ; **không** gồ ghề nhiều / không sần sùi cả mảng má; **không** đỏ sưng, **không** đầu trắng / mủ.
→ Prose: **“Má của mày đang có nhiều nốt nhỏ màu da nổi cao, trông giống mụn ẩn hoặc milia. Không thấy đỏ sưng hay mủ.”**
→ **CẤM tuyệt đối** “mụn thịt” / skin tag trên má. **CẤM** gọi Nốt đỏ sưng / kích ứng khi không đỏ.
→ “trông giống mụn ẩn hoặc milia” là cụm **bắt buộc / được phép**, không phải hedge. Được nói **milia**.
→ Hướng xử lý: không tự cắt / nặn / chà mạnh; muốn xử lý → cơ sở y tế / da liễu. **CẤM** tip trị mụn đỏ / BHA.

### B — Sần sùi / texture không đều (nhiều nốt + bề mặt gồ)
Dấu hiệu: má **sần sùi rõ**; nhiều nốt nhỏ nổi **và** bề mặt da **gồ ghề không đều**; có thể kèm thâm/lõm nông; **không** đỏ sưng, **không** mủ.
→ Prose: **“Má của mày đang sần sùi rõ, nhiều nốt nhỏ nổi cao + bề mặt da gồ ghề không đều. Không thấy đỏ sưng hay mủ.”** (Có thâm nông thì nói; **CẤM** phủ định thâm khi có.)
→ **CẤM** “Nốt đỏ sưng”. **CẤM** mụn thịt. **CẤM** chỉ gọi milia khi bề mặt đã sần/gồ rõ.
→ Nguyên nhân hướng: **“Do mụn ẩn lâu ngày + texture da bị ảnh hưởng.”**
→ Hướng xử lý: không tự cắt / nặn / chà mạnh; muốn xử lý → cơ sở y tế / da liễu; **CẤM** tip trị mụn đỏ / BHA.

Lý do phân loại: mụn thịt thường **mềm, có cuống hoặc dẹt**, hay gặp **cổ / nách / mí mắt**. Nốt tròn mịn trên má → mụn ẩn/milia. Mảng sần + gồ không đều → texture sau mụn ẩn lâu.

## Nếp gấp / nếp ngang cổ (BẮT BUỘC — tách khỏi mụn thịt)
Khi ảnh hoặc user_question khớp ảnh **vùng cổ**, **nếp ngang / nếp gấp** rõ, **không** cụm nốt đỏ sưng, **không** nốt màu da nổi cao:
→ Prose: **“nếp gấp / nếp ngang cổ”**. Góc/nắng có thể làm nếp đọc rõ hơn — nói 1 câu được, **CẤM** phủ nhận hết (“chẳng có gì”, “khá ổn”).
→ **CẤM** default mụn / mụn thịt / kích ứng khi không có dấu đó.
→ Nguyên nhân: tư thế cúi lâu (điện thoại/máy tính) / da cổ dễ lộ nếp + nắng — **CẤM** “do kích ứng” suông; **CẤM** tự chốt bệnh tuyến giáp / **u tuyến giáp** từ ảnh nếp da.
→ Hướng xử lý: **Giảm cúi điện thoại lâu** / chỉnh tư thế; chống nắng cả cổ; dưỡng ẩm cổ; massage nhẹ optional; **CẤM** hứa hết nếp nhanh; **CẤM** tip “đừng nặn / đỏ sưng”.

## Mụn thịt / nốt cổ–nách (BẮT BUỘC — không phải mụn viêm mặt)
Khi nốt nhỏ màu da / nâu nhạt, nổi cao, **không** đỏ sưng / không mủ, vùng **cổ, nách, mí mắt** (nốt **mềm, có cuống hoặc dẹt**), **ảnh không phải má**:
→ Prose: **“trông giống mụn thịt”**.
→ **CẤM** default “kích ứng nhẹ” khi không có đỏ–ngứa–viêm rõ.
→ **CẤM tuyệt đối** gọi mụn thịt cho **cụm dày trên má** — đó là mụn ẩn hoặc milia.
→ Hướng xử lý: không tẩy–chà mạnh–tự cắt/nặn; muốn lấy bỏ → cơ sở y tế / da liễu; **CẤM** tip trị mụn đỏ / BHA / hứa hết bằng mỹ phẩm.

## Khóe miệng / mép — TÁCH 2 CASE (BẮT BUỘC)
Chỉ nhìn gần miệng **không** đủ để gọi viêm cấp.

### A — Thâm / sẫm khóe miệng–cằm (KHÔNG viêm cấp)
Màu nâu / xám sẫm hơn nền quanh khóe miệng / dưới cằm; bề mặt **phẳng hoặc ít nổi**; không chùm hạt đỏ sưng.
→ Prose: **thâm / sắc tố quanh miệng** hoặc **thâm sau mụn** quanh khóe–cằm.
→ Hướng xử lý: chống nắng, dịu da; muốn trị chuyên sâu → khám da liễu — **CẤM** tip “không nặn / viêm cấp”.
→ **CẤM tuyệt đối**: “viêm cấp sát mép miệng”, template herpes/lở miệng, “Đây là mụn có mủ”.

### B — Viêm cấp sát mép môi (rule hẹp — chỉ khi đủ tín hiệu)
Đỏ sưng rõ / chùm hạt nổi sát viền môi, **và** lên nhanh / đau–chằn khi há miệng **hoặc** ảnh rõ chùm đỏ sưng cấp.
→ Kết luận: **“viêm cấp sát mép miệng”** / **“chùm hạt đỏ sưng ngay viền môi”**.
→ **CẤM** “Đây là mụn có mủ”; **CẤM** herpes chắc / thuốc kháng virus.
→ Hướng xử lý: không nặn/bóc/chạm; đau tăng / lan / tái → khám da liễu.
→ **Không** áp nhánh B chỉ vì crop gần miệng hoặc user nói “mép môi” khi đang hỏi **thâm**.

### Ưu tiên khi A và B tranh
- Ảnh có **chùm hạt đỏ sưng rõ** sát viền môi → chọn **B**, kể cả user có chữ “thâm”.
- User hỏi **thâm** quanh mép/cằm **và** ảnh **không** có chùm đỏ sưng cấp → chọn **A**.
- Chỉ chữ “mép môi” trên crop gần miệng **không** đủ để chọn B.

## Thâm / dấu cũ (BẮT BUỘC)
- Nếu **vùng đang viết** có điểm thâm / nốt cũ nông → **CẤM** “không thấy thâm”, “chưa thấy thâm”.
- Dùng: “thâm rất nhẹ”, “vài điểm thâm nông”, “xen vài vết thâm nông”.
- Chỉ phủ định thâm khi **đúng vùng đó** thật sự không có dấu thâm.

## Trái / phải má (BẮT BUỘC)
- Xác định theo **tai / tóc mai của người trong ảnh**, KHÔNG theo trái/phải khung (selfie/mirror).
- Không chắc → viết **“má của mày”** — **CẤM đoán** má phải/má trái.

## Chỉ nhận xét vùng nhìn thấy (BẮT BUỘC)
- Crop / close-up / ảnh cổ–thân: chỉ mô tả vùng **có trên khung**. **CẤM** bịa “trán yên / cằm ổn / da mặt dầu” khi không thấy.
- Ảnh cổ / thân không mặt: đừng quy thành mụn mặt. Nếp ngang phẳng → nếp gấp cổ; nốt màu da nổi cao → mụn thịt — **CẤM** nhầm hai case.
- Heuristic: dải da mặt không thấy môi → trán, không gọi cằm. Dải da cổ/nếp gấp → cổ, **CẤM** gọi trán.

## Hướng xử lý khớp nhóm (BẮT BUỘC — dưỡng/trị phải theo đúng hình thái)
- **Đỏ sưng / mụn viêm**: không nặn; rửa dịu; tạm nghỉ mạnh *chỉ* khi đang kích.
- **Mụn ẩn thuần** (ít/không đỏ): sạch dịu + ẩm + chống nắng; **CẤM** phủ nhận mụn ẩn; **CẤM** hứa hết nhanh.
- **Má milia / nốt màu da tròn mịn**: không cắt/nặn/chà; khám nếu muốn xử lý; **CẤM** mụn thịt; **CẤM** BHA trị mụn đỏ.
- **Má sần sùi / texture không đều**: như trên; **CẤM** chỉ gọi milia khi đã sần/gồ; **CẤM** BHA trị mụn đỏ.
- **Nốt nhỏ + đỏ hồng rõ**: dịu trước (sạch dịu, ẩm, tránh acid/retinol/chà); **CẤM** đẩy BHA ngay.
- **Thâm / sắc tố / khóe miệng nhánh A**: chống nắng + dịu; **CẤM** tip viêm cấp / không nặn khi không ổ sưng.
- **Viêm cấp sát mép (nhánh B)**: không nặn/bóc; **CẤM** trị như mụn có mủ.
- **Mụn thịt cổ–nách–mí**: không tẩy/cắt DIY; **CẤM** dùng case này cho má.
- **Nếp gấp cổ**: tư thế + chống nắng cổ + dưỡng ẩm; **CẤM** dọa u tuyến giáp nếu chỉ thấy nếp.
- **Lỗ chân lông to**: sạch + dưỡng; chỉ nói **trông đỡ to** khi sạch/mịn hơn — **CẤM** “se khít / se lỗ chân lông”.

Sai nhóm hình thái = FAIL. Gọi mụn thịt trên má = FAIL. Gọi Nốt đỏ sưng khi không đỏ = FAIL. Phủ nhận mụn ẩn khi nốt li ti rõ = FAIL. “Không viêm” khi đỏ hồng + nốt nhỏ = FAIL.`
}

// VisionMorphologyCoachGuard is for text coaches that do not see the photo.
// They must keep vision's morphology group and not invent a different diagnosis
// that would drive the wrong routine / actives.
func VisionMorphologyCoachGuard() string {
	return `## Hình thái từ vision (BẮT BUỘC — đừng “sửa” sai)
Bạn KHÔNG nhìn ảnh. VISION_SUMMARY_JSON / detailed_observations / visible_observations đã gọi tên nhóm thì **GIỮ đúng nhóm đó** khi viết nhận xét, tips, routine, care_suggestions.
Sai tên nhóm → dưỡng/trị sai.

- Má **mụn ẩn / milia** (nốt màu da, không đỏ) → CẤM đổi thành mụn viêm / mụn thịt / Nốt đỏ sưng. CẤM đẩy BHA/retinol như trị mụn đỏ.
- Má **sần sùi / texture không đều / gồ ghề** (không đỏ) → CẤM khuyên như mụn đỏ sưng; CẤM BHA ngay; CẤM chỉ gọi milia nếu vision đã nói sần/gồ.
- **Mụn ẩn thuần** (ít/không đỏ) → acknowledge mụn ẩn; sạch dịu + ẩm + chống nắng.
- Nốt nhỏ **+ đỏ hồng rõ** → acknowledge **mụn ẩn + kích ứng/viêm nhẹ**; CẤM “chỉ mụn ẩn” / “không viêm”; **CẤM** BHA ngay — dịu trước.
- **Thâm quanh miệng / khóe–cằm** (phẳng, không chùm đỏ) → CẤM “viêm cấp sát mép” / tip đừng nặn ổ sưng.
- **Viêm cấp sát mép** (chùm hạt đỏ sưng) → CẤM trị như mụn có mủ trên má.
- **Mụn thịt** cổ–nách–mí → CẤM trị như mụn mặt / BHA; CẤM gọi mụn thịt cho má.
- **Nếp gấp / nếp ngang cổ** → CẤM default mụn thịt / kích ứng; CẤM dọa u tuyến giáp.
- CẤM bịa vùng không có trong vision.`
}

// OnboardingMorphologyJSONMap maps shared groups onto onboarding vision JSON.
func OnboardingMorphologyJSONMap() string {
	return `## Map nhóm hình thái → JSON onboarding (sau khi đã chọn đúng nhóm ở trên)
- mụn viêm / có mủ / bọc → concern_types: inflammatory_acne; main_concerns: "mụn viêm" / "nốt đỏ"; acne_status: inflammatory_acne|cystic_acne; phase thường calm_first nếu đỏ sưng rõ
- mụn ẩn thuần → concern_types: **comedones**; main_concerns: "mụn ẩn"; acne_status: few_whiteheads; texture bumpy/slightly_rough; **CẤM** inflammatory_acne; **CẤM phủ nhận** mụn ẩn
- milia / nốt màu da tròn mịn trên má → comedones; main_concerns: "mụn ẩn"; acne_status: few_whiteheads; **CẤM** inflammatory_acne; **CẤM** gọi mụn thịt
- sần sùi / gồ không đều, không đỏ → concern_types: **texture**; main_concerns: "da sần" / "da không mịn đều"; texture: rough|bumpy; **CẤM** inflammatory_acne; redness: none
- nốt nhỏ + đỏ hồng rõ → comedones + redness_irritation (kèm inflammatory_acne nếu nốt đỏ sưng rõ); main_concerns: "mụn ẩn", "da đỏ"; phase **calm_first**; prose **vừa mụn ẩn vừa kích ứng**/viêm nhẹ; **CẤM** đẩy BHA/retinol
- thâm / sắc tố / khóe miệng nhánh A → concern_types: pih; primary_regions gồm perioral nếu quanh miệng; main_concerns: "thâm"; **CẤM** redness_irritation nếu không đỏ sưng cấp
- viêm cấp sát mép (nhánh B) → redness_irritation; perioral; phase calm_first
- mụn thịt cổ–nách–mí (hiếm ở onboarding mặt) → mô tả trong detailed_observations; **CẤM** map thành mụn viêm mặt / inflammatory_acne
- nếp gấp cổ → wrinkles hoặc texture; **CẤM** inflammatory_acne
- phase **calm_first** chỉ khi đỏ sưng / dense / kích ứng rõ — **CẤM** calm_first chỉ vì milia/sần không đỏ
- detailed_observations + main_concerns: lời đời thường, đủ nhóm đúng; CẤM papules/pustules/comedone/texture Anh trong prose`
}

// CheckInMorphologyJSONMap maps shared groups onto daily check-in vision JSON.
func CheckInMorphologyJSONMap() string {
	return `## Map nhóm hình thái → JSON check-in (sau khi đã chọn đúng nhóm)
visible_observations: mỗi bullet ngắn = **vùng + tên nhóm đúng + mức**. Dùng lời Việt đời thường: mụn viêm, mụn có mủ, mụn ẩn, milia, sần sùi / texture không đều, thâm quanh miệng, viêm cấp sát mép, mụn thịt (chỉ cổ–nách–mí), nếp gấp cổ…
- Má nốt màu da tròn mịn không đỏ → “má: trông giống mụn ẩn hoặc milia” — **CẤM** mụn thịt, **CẤM** nốt đỏ sưng
- Má sần/gồ không đỏ → “má: sần sùi / gồ ghề không đều”
- Nốt nhỏ + đỏ hồng → nói cả mụn ẩn lẫn kích ứng/viêm nhẹ
- ≤5 bullets nhưng **tên nhóm phải đúng** — sai tên làm coach dưỡng/trị sai
texture_and_oil_cues / redness_or_discoloration_cues: khớp nhóm; không đỏ thì đừng viết redness như viêm cấp`
}

// AdminMorphologyJSONMap maps shared groups onto admin skin-review JSON enums.
func AdminMorphologyJSONMap() string {
	return `## Map nhóm hình thái → JSON admin (enum concern)
Enum JSON: acne|papules|pustules|irritation|pigmentation|texture|other… Prose vẫn dùng tên nhóm đời thường.
- mụn ẩn thuần (ít/không đỏ) → concern ` + "`acne`" + ` — **CẤM** ` + "`papules`" + ` (label “Nốt đỏ sưng”)
- milia / nốt màu da tròn mịn trên má → ` + "`acne`" + ` — **CẤM** papules / pustules / redness / irritation / mụn thịt
- sần sùi / gồ không đều, không đỏ → ` + "`texture`" + `; severity moderate|pronounced — **CẤM** papules / “Nốt đỏ sưng”; possible_causes **phải** có “Do mụn ẩn lâu ngày + texture da bị ảnh hưởng.”
- nốt nhỏ + đỏ hồng rõ → redness|irritation **và** papules|acne
- thâm quanh miệng → pigmentation|dark_spots
- viêm cấp sát mép → irritation
- mụn thịt cổ–nách–mí → other; region neck|other
- nếp gấp cổ → texture; region neck
- ` + "`pustules`" + ` **chỉ khi** prose thấy đầu trắng / mủ rõ
- **CẤM papules / “Nốt đỏ sưng”** khi prose nói không đỏ sưng`
}
