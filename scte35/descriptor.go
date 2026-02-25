package scte35

const (
	// SegmentationDescriptorTag is the splice_descriptor_tag for segmentation_descriptor.
	SegmentationDescriptorTag uint32 = 0x02

	// CUEIdentifier is the CUEI ASCII identifier (0x43554549).
	CUEIdentifier uint32 = 0x43554549
)

// Segmentation type constants per SCTE-35 Table 22.
const (
	SegmentationTypeNotIndicated              uint32 = 0x00
	SegmentationTypeContentIdentification     uint32 = 0x01
	SegmentationTypeProgramStart              uint32 = 0x10
	SegmentationTypeProgramEnd                uint32 = 0x11
	SegmentationTypeProgramEarlyTermination   uint32 = 0x12
	SegmentationTypeProgramBreakaway          uint32 = 0x13
	SegmentationTypeProgramResumption         uint32 = 0x14
	SegmentationTypeProgramRunoverPlanned     uint32 = 0x15
	SegmentationTypeProgramRunoverUnplanned   uint32 = 0x16
	SegmentationTypeProgramOverlapStart       uint32 = 0x17
	SegmentationTypeProgramBlackoutOverride   uint32 = 0x18
	SegmentationTypeProgramStartInProgress    uint32 = 0x19
	SegmentationTypeChapterStart              uint32 = 0x20
	SegmentationTypeChapterEnd                uint32 = 0x21
	SegmentationTypeBreakStart                uint32 = 0x22
	SegmentationTypeBreakEnd                  uint32 = 0x23
	SegmentationTypeOpeningCreditStart        uint32 = 0x24
	SegmentationTypeOpeningCreditEnd          uint32 = 0x25
	SegmentationTypeClosingCreditStart        uint32 = 0x26
	SegmentationTypeClosingCreditEnd          uint32 = 0x27
	SegmentationTypeProviderAdStart           uint32 = 0x30
	SegmentationTypeProviderAdEnd             uint32 = 0x31
	SegmentationTypeDistributorAdStart        uint32 = 0x32
	SegmentationTypeDistributorAdEnd          uint32 = 0x33
	SegmentationTypeProviderPOStart           uint32 = 0x34
	SegmentationTypeProviderPOEnd             uint32 = 0x35
	SegmentationTypeDistributorPOStart        uint32 = 0x36
	SegmentationTypeDistributorPOEnd          uint32 = 0x37
	SegmentationTypeProviderOverlayPOStart    uint32 = 0x38
	SegmentationTypeProviderOverlayPOEnd      uint32 = 0x39
	SegmentationTypeDistributorOverlayPOStart uint32 = 0x3a
	SegmentationTypeDistributorOverlayPOEnd   uint32 = 0x3b
	SegmentationTypeProviderPromoStart        uint32 = 0x3c
	SegmentationTypeProviderPromoEnd          uint32 = 0x3d
	SegmentationTypeDistributorPromoStart     uint32 = 0x3e
	SegmentationTypeDistributorPromoEnd       uint32 = 0x3f
	SegmentationTypeUnscheduledEventStart     uint32 = 0x40
	SegmentationTypeUnscheduledEventEnd       uint32 = 0x41
	SegmentationTypeAltConOppStart            uint32 = 0x42
	SegmentationTypeAltConOppEnd              uint32 = 0x43
	SegmentationTypeProviderAdBlockStart      uint32 = 0x44
	SegmentationTypeProviderAdBlockEnd        uint32 = 0x45
	SegmentationTypeDistributorAdBlockStart   uint32 = 0x46
	SegmentationTypeDistributorAdBlockEnd     uint32 = 0x47
	SegmentationTypeNetworkStart              uint32 = 0x50
	SegmentationTypeNetworkEnd                uint32 = 0x51
)

// SegmentationDescriptor carries segmentation information per SCTE-35 10.3.3.
type SegmentationDescriptor struct {
	SegmentationEventID  uint32
	SegmentationTypeID   uint32
	SegmentationDuration *uint64
	SegmentNum           uint32
	SegmentsExpected     uint32
}

// Tag returns the splice_descriptor_tag.
func (sd *SegmentationDescriptor) Tag() uint32 {
	return SegmentationDescriptorTag
}

// Name returns a human-readable name for the segmentation type.
func (sd *SegmentationDescriptor) Name() string {
	switch sd.SegmentationTypeID {
	case SegmentationTypeNotIndicated:
		return "Not Indicated"
	case SegmentationTypeContentIdentification:
		return "Content Identification"
	case SegmentationTypeProgramStart:
		return "Program Start"
	case SegmentationTypeProgramEnd:
		return "Program End"
	case SegmentationTypeProgramEarlyTermination:
		return "Program Early Termination"
	case SegmentationTypeProgramBreakaway:
		return "Program Breakaway"
	case SegmentationTypeProgramResumption:
		return "Program Resumption"
	case SegmentationTypeProgramRunoverPlanned:
		return "Program Runover Planned"
	case SegmentationTypeProgramRunoverUnplanned:
		return "Program Runover Unplanned"
	case SegmentationTypeProgramOverlapStart:
		return "Program Overlap Start"
	case SegmentationTypeProgramBlackoutOverride:
		return "Program Blackout Override"
	case SegmentationTypeProgramStartInProgress:
		return "Program Start - In Progress"
	case SegmentationTypeChapterStart:
		return "Chapter Start"
	case SegmentationTypeChapterEnd:
		return "Chapter End"
	case SegmentationTypeBreakStart:
		return "Break Start"
	case SegmentationTypeBreakEnd:
		return "Break End"
	case SegmentationTypeOpeningCreditStart:
		return "Opening Credit Start"
	case SegmentationTypeOpeningCreditEnd:
		return "Opening Credit End"
	case SegmentationTypeClosingCreditStart:
		return "Closing Credit Start"
	case SegmentationTypeClosingCreditEnd:
		return "Closing Credit End"
	case SegmentationTypeProviderAdStart:
		return "Provider Advertisement Start"
	case SegmentationTypeProviderAdEnd:
		return "Provider Advertisement End"
	case SegmentationTypeDistributorAdStart:
		return "Distributor Advertisement Start"
	case SegmentationTypeDistributorAdEnd:
		return "Distributor Advertisement End"
	case SegmentationTypeProviderPOStart:
		return "Provider Placement Opportunity Start"
	case SegmentationTypeProviderPOEnd:
		return "Provider Placement Opportunity End"
	case SegmentationTypeDistributorPOStart:
		return "Distributor Placement Opportunity Start"
	case SegmentationTypeDistributorPOEnd:
		return "Distributor Placement Opportunity End"
	case SegmentationTypeProviderOverlayPOStart:
		return "Provider Overlay Placement Opportunity Start"
	case SegmentationTypeProviderOverlayPOEnd:
		return "Provider Overlay Placement Opportunity End"
	case SegmentationTypeDistributorOverlayPOStart:
		return "Distributor Overlay Placement Opportunity Start"
	case SegmentationTypeDistributorOverlayPOEnd:
		return "Distributor Overlay Placement Opportunity End"
	case SegmentationTypeProviderPromoStart:
		return "Provider Promo Start"
	case SegmentationTypeProviderPromoEnd:
		return "Provider Promo End"
	case SegmentationTypeDistributorPromoStart:
		return "Distributor Promo Start"
	case SegmentationTypeDistributorPromoEnd:
		return "Distributor Promo End"
	case SegmentationTypeUnscheduledEventStart:
		return "Unscheduled Event Start"
	case SegmentationTypeUnscheduledEventEnd:
		return "Unscheduled Event End"
	case SegmentationTypeAltConOppStart:
		return "Alternate Content Opportunity Start"
	case SegmentationTypeAltConOppEnd:
		return "Alternate Content Opportunity End"
	case SegmentationTypeProviderAdBlockStart:
		return "Provider Ad Block Start"
	case SegmentationTypeProviderAdBlockEnd:
		return "Provider Ad Block End"
	case SegmentationTypeDistributorAdBlockStart:
		return "Distributor Ad Block Start"
	case SegmentationTypeDistributorAdBlockEnd:
		return "Distributor Ad Block End"
	case SegmentationTypeNetworkStart:
		return "Network Start"
	case SegmentationTypeNetworkEnd:
		return "Network End"
	default:
		return "Unknown"
	}
}

func (sd *SegmentationDescriptor) decode(data []byte) error {
	r := newBitReader(data)
	r.skip(8)  // splice_descriptor_tag
	r.skip(8)  // descriptor_length
	r.skip(32) // identifier (CUEI)
	sd.SegmentationEventID = r.readUint32(32)
	cancelIndicator := r.readBit()
	r.skip(1) // segmentation_event_id_compliance_indicator
	r.skip(6) // reserved

	if !cancelIndicator {
		programSegmentationFlag := r.readBit()
		durationFlag := r.readBit()
		deliveryNotRestricted := r.readBit()

		if !deliveryNotRestricted {
			r.skip(5) // restriction flags
		} else {
			r.skip(5) // reserved
		}

		if !programSegmentationFlag {
			componentCount := int(r.readUint32(8))
			for i := 0; i < componentCount; i++ {
				r.skip(8)  // component_tag
				r.skip(7)  // reserved
				r.skip(33) // pts_offset
			}
		}

		if durationFlag {
			dur := r.readUint64(40)
			sd.SegmentationDuration = &dur
		}

		r.skip(8)                       // segmentation_upid_type
		upidLen := int(r.readUint32(8)) // segmentation_upid_length
		r.skip(upidLen * 8)             // skip UPID bytes
		sd.SegmentationTypeID = r.readUint32(8)
		sd.SegmentNum = r.readUint32(8)
		sd.SegmentsExpected = r.readUint32(8)

		// Skip optional sub-segment fields if present.
		if r.bitsLeft() >= 16 {
			r.skip(16)
		}
	}
	return nil
}

func (sd *SegmentationDescriptor) encode() ([]byte, error) {
	length := sd.descriptorLength()
	w := newBitWriter(length + 2) // +2 for tag + length fields

	w.putUint32(8, SegmentationDescriptorTag)
	w.putUint32(8, uint32(length))
	w.putUint32(32, CUEIdentifier)
	w.putUint32(32, sd.SegmentationEventID)
	w.putBit(false)      // segmentation_event_cancel_indicator = 0
	w.putBit(true)       // segmentation_event_id_compliance_indicator (inverted: false → bit 1)
	w.putUint32(6, 0x3F) // reserved

	w.putBit(true)                           // program_segmentation_flag = 1
	w.putBit(sd.SegmentationDuration != nil) // segmentation_duration_flag
	w.putBit(true)                           // delivery_not_restricted_flag = 1
	w.putUint32(5, 0x1F)                     // reserved

	if sd.SegmentationDuration != nil {
		w.putUint64(40, *sd.SegmentationDuration)
	}

	w.putUint32(8, 0x00) // segmentation_upid_type = Not Used
	w.putUint32(8, 0x00) // segmentation_upid_length = 0
	w.putUint32(8, sd.SegmentationTypeID)
	w.putUint32(8, sd.SegmentNum)
	w.putUint32(8, sd.SegmentsExpected)

	return w.bytes(), nil
}

func (sd *SegmentationDescriptor) descriptorLength() int {
	bits := 32 // identifier
	bits += 32 // segmentation_event_id
	bits += 1  // cancel_indicator
	bits += 1  // compliance_indicator
	bits += 6  // reserved

	// cancel=false, so remaining fields are present:
	bits += 1 // program_segmentation_flag
	bits += 1 // segmentation_duration_flag
	bits += 1 // delivery_not_restricted_flag
	bits += 5 // reserved (delivery_not_restricted=true)

	if sd.SegmentationDuration != nil {
		bits += 40
	}

	bits += 8 // segmentation_upid_type
	bits += 8 // segmentation_upid_length (0)
	bits += 8 // segmentation_type_id
	bits += 8 // segment_num
	bits += 8 // segments_expected

	return bits / 8
}
